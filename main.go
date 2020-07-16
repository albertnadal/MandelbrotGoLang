/*

- RUN ON A SINGLE COMPUTER:
go run main.go

- RUN USING DISTRIBUTED COMPUTING:

Run as master:
go run main.go --role=master --slaves=127.0.0.1

Run as slave:
go run main.go --role=slave

*/

package main

import (
	"flag"
	"fmt"
	"context"
	"github.com/gen2brain/raylib-go/raygui"
	"github.com/gen2brain/raylib-go/raylib"
	"github.com/lucasb-eyer/go-colorful"
	"google.golang.org/grpc"
	//"google.golang.org/grpc/keepalive"
	"log"
	"math"
	//"os"
	"net"
	"runtime"
	//"runtime/pprof"
	"strings"
	"sync"
	"time"
	"mandelbrot-fractal/proto"
)

const DEBUG bool = false
//const PROFILE bool = false

const MAX_THREADS int32 = 8
const SCREEN_WIDTH int32 = 1280 / 2
const SCREEN_HEIGHT int32 = 720 / 2

type Mandelbrot struct {
	ScreenWidth          int32
	ScreenHeight         int32
	Pixels               [][]rl.Color
	MagnificationFactor  float64
	MaxIterations        float64
	PanX                 float64
	PanY                 float64
	ThreadWaitGroup      sync.WaitGroup
	DistributedWaitGroup sync.WaitGroup
	NeedUpdate           bool
	MaxThreads           int32
	ThreadsProcessTimes  []time.Duration
	TotalProcessTime     time.Duration
	ZoomLevel            float64
	Canvas               rl.RenderTexture2D
	MovementOffset       [16]float64
	IsMaster             bool
	SlavePort            int32
	SlavesClients        []proto.MandelbrotSlaveNodeClient // Used only in 'master' mode
	SlavesCount          int32
	RegionWidth          int32
	RegionHeight         int32
	FragmentWidth        int32
	FragmentHeight       int32
	RGBBuffer            []byte
}

var nodeRole = flag.String("role", "master", "cluster node role: `master` or `slave`")
var slavesIPs = flag.String("slaves", "", "cluster node slaves IP's separated by comas")
//var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")
//var memprofile = flag.String("memprofile", "", "write memory profile to `file`")

func main() {

	flag.Parse()

/*	if PROFILE {
		if *cpuprofile != "" {
			f, err := os.Create(*cpuprofile)
			if err != nil {
				log.Fatal("could not create CPU profile: ", err)
			}
			defer f.Close()
			if err := pprof.StartCPUProfile(f); err != nil {
				log.Fatal("could not start CPU profile: ", err)
			}
			defer pprof.StopCPUProfile()
		}
	}*/

	// Ask the Golang runtime how many CPU cores are available
	totalCores := runtime.NumCPU()
	isMaster := (*nodeRole != "slave")
	fmt.Printf("\n- Multi-threaded cores available: %d\n", totalCores)
	fmt.Printf("- Using %d cores\n", totalCores)
	var slaves []string

	if len(*slavesIPs) > 0 {
		slaves = strings.Split(*slavesIPs, ",")
	}

	if isMaster {
		fmt.Println("- Running as master")
		fmt.Println("- Slaves:", slaves)
	} else {
		fmt.Println("- Running as slave")
	}

	// Set-up the Go runtime to use all the available CPU cores
	runtime.GOMAXPROCS(totalCores)

	if isMaster {
		rl.InitWindow(SCREEN_WIDTH, SCREEN_HEIGHT, "Mandelbrot fractal")
		rl.SetTargetFPS(30)
	}

	fractal := Mandelbrot{}
	fractal.Init(isMaster, slaves)

	if isMaster {
		fmt.Println("\n- Use keys A and S for zoom-in and zoom-out.")
		fmt.Println("- Use arrow keys to navigate.\n")

		for !rl.WindowShouldClose() {
			fractal.Update()
			fractal.Draw()
			fractal.ProcessKeyboard()
		}

		rl.UnloadTexture(fractal.Canvas.Texture)
		rl.CloseWindow()
	} else {
		fractal.ProcessRequestsFromMasterNode()
	}
/*
	if PROFILE {
		if *memprofile != "" {
			f, err := os.Create(*memprofile)
			if err != nil {
				log.Fatal("could not create memory profile: ", err)
			}
			defer f.Close()
			runtime.GC() // get up-to-date statistics
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.Fatal("could not write memory profile: ", err)
			}
		}
	}
*/
}

// Mandelbrot functions

func (m *Mandelbrot) Init(isMaster bool, slavesIPs []string) {
	m.ScreenWidth = SCREEN_WIDTH * 2
	m.ScreenHeight = SCREEN_HEIGHT * 2
	m.ZoomLevel = 0.1
	m.MagnificationFactor = 400
	m.MaxIterations = 80
	m.PanX = 1.624203
	m.PanY = 0.620820
	m.MovementOffset = [...]float64{
		0.018666, 0.017666, 0.016666, 0.015000,
		0.002950, 0.000400, 0.000025, 0.0000025,
		0.00000025, 0.000000025, 0.0000000025, 0.0000000025,
		0.00000000025, 0.000000000025, 0.0000000000025, 0.00000000000025}
	m.NeedUpdate = true
	m.MaxThreads = MAX_THREADS
	m.ThreadsProcessTimes = make([]time.Duration, m.MaxThreads)
	m.FragmentWidth = int32(math.Ceil(float64(m.ScreenWidth - 1) / float64(m.MaxThreads)))
	m.FragmentHeight = m.ScreenHeight - 1
	m.SlavePort = 50051
	m.IsMaster = isMaster

	if m.IsMaster {
		m.Canvas = rl.LoadRenderTexture(m.ScreenWidth, m.ScreenHeight)
		m.SlavesCount = int32(len(slavesIPs))
		m.SlavesClients = make([]proto.MandelbrotSlaveNodeClient, m.SlavesCount)
		m.RegionWidth = int32(math.Ceil(float64(m.ScreenWidth - 1) / float64(m.SlavesCount + 1)))
		m.RegionHeight = m.ScreenHeight

		for c := int32(0); c < m.SlavesCount; c++ {
			address := fmt.Sprintf("%s:%d", slavesIPs[c], m.SlavePort)
			fmt.Printf("- Connecting to slave node at %s... ", address)
			conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
			if err != nil {
				log.Fatalf(" [ ERROR ] Cannot connect: %v", err)
			}
			fmt.Print("[ OK ]\n")
			m.SlavesClients[c] = proto.NewMandelbrotSlaveNodeClient(conn)
		}
	}

	m.Pixels = make([][]rl.Color, m.ScreenWidth)
	for i := int32(0); i < m.ScreenWidth; i++ {
		m.Pixels[i] = make([]rl.Color, m.ScreenHeight)
	}

	for x := int32(0); x < m.ScreenWidth; x++ {
		for y := int32(0); y < m.ScreenHeight; y++ {
			m.Pixels[x][y] = rl.NewColor(0, 0, 0, 255)
		}
	}
}

func (m *Mandelbrot) Update() {
	if !m.NeedUpdate {
		return
	}

	start := time.Now()

	if(m.SlavesCount == 0) {
		// SINGLE COMPUTER
		for i := int32(0); i < m.MaxThreads; i++ {
			m.ThreadWaitGroup.Add(1)
			go m.CalculateFragmentInThread(i, i*m.FragmentWidth, 0, i*m.FragmentWidth+m.FragmentWidth+i, m.FragmentHeight, 0, m.FragmentWidth)
		}
		m.ThreadWaitGroup.Wait()

	} else {
		// DISTRIBUTED COMPUTING
		regionIndex := int32(0)
		for regionIndex = 0; regionIndex < m.SlavesCount; regionIndex++ {
			// Calculate every region remotelly in slave nodes
			m.DistributedWaitGroup.Add(1)
			go m.CalculateRegionInSlaveNode(regionIndex, regionIndex*m.RegionWidth+1, 0, regionIndex*m.RegionWidth + m.RegionWidth, m.RegionHeight)
		}

		// Calculate one region locally (master node)
		start := time.Now()
		m.CalculateRegionLocally(regionIndex*m.RegionWidth+1, 0, regionIndex*m.RegionWidth+m.RegionWidth, m.RegionHeight)
		fmt.Printf("(master) (time %s)\n", time.Since(start))

		// Wait for all distributed calculations
		m.DistributedWaitGroup.Wait()
	}

	m.TotalProcessTime = time.Since(start)
}

func (m *Mandelbrot) CalculateRegionInSlaveNode(region_index int32, x_start int32, y_start int32, x_end int32, y_end int32) {
  defer m.DistributedWaitGroup.Done()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()
	response, err := m.SlavesClients[region_index].CalculateRegion(ctx, &proto.CalculateRegionRequest{MagnificationFactor: m.MagnificationFactor, MaxIterations: m.MaxIterations, PanX: m.PanX, PanY: m.PanY, RegionWidth: m.RegionWidth, RegionHeight: m.RegionHeight, RegionIndex: region_index})
	if err != nil {
		log.Fatalf("An error occurred when fetching data from slave node (%d) error: (%v)", region_index, err)
	}
	fmt.Printf("(slave %d) (time %s)\n", region_index, time.Since(start))

	rgbBuffer := response.GetRGBPixels()

	var i int32 = 0
	for x := x_start; (x <= x_end) && (x < m.ScreenWidth); x++ {
		for y := y_start; y < y_end; y++ {
			m.Pixels[x][y] = rl.NewColor(rgbBuffer[i*3], rgbBuffer[i*3+1], rgbBuffer[i*3+2], 255) // RGBA
			i++
		}
	}
}

func (m *Mandelbrot) CalculateRegionLocally(x_start int32, y_start int32, x_end int32, y_end int32) {
	regionWidth := x_end - x_start
	fragmentWidth := int32(math.Ceil(float64(regionWidth) / float64(m.MaxThreads)))
  fragmentHeight := y_end - y_start

	for i := int32(0); i < m.MaxThreads; i++ {
		m.ThreadWaitGroup.Add(1)
		go m.CalculateFragmentInThread(i, x_start + i*fragmentWidth, y_start, x_start + i*fragmentWidth + fragmentWidth, fragmentHeight, i*fragmentWidth*fragmentHeight, x_end)
	}

	m.ThreadWaitGroup.Wait()
}

func (m *Mandelbrot) CalculateFragmentInThread(thread_index int32, x_start int32, y_start int32, x_end int32, y_end int32, offset int32, x_region_end int32) {
	defer m.ThreadWaitGroup.Done()

	start := time.Now()
	var red, green, blue uint8
	var i int32 = 0

	for x := x_start; (x <= x_end) && (x < x_region_end); x++ {
		for y := y_start; y < y_end; y++ {
			//calc_start := time.Now()
			red, green, blue = m.GetPixelColorAtPosition((float64(x)/m.MagnificationFactor)-m.PanX, (float64(y)/m.MagnificationFactor)-m.PanY)
			if(m.IsMaster) {
				// RGBA matrix to draw the fractan and show in the window
				m.Pixels[x][y] = rl.NewColor(red, green, blue, 255)
			} else {
				// RBG buffer used to store the data that should be sent to the master node
				m.RGBBuffer[offset*3 + i*3] = red
				m.RGBBuffer[offset*3 + i*3 + 1] = green
				m.RGBBuffer[offset*3 + i*3 + 2] = blue
			}
			i++
		}
	}
	m.ThreadsProcessTimes[thread_index] = time.Since(start)
}

func (m *Mandelbrot) GetPixelColorAtPosition(x float64, y float64) (uint8, uint8, uint8) {
	realComponent := x
	imaginaryComponent := y
	var tempRealComponent float64

	for i := float64(0); i < m.MaxIterations; i++ {
		tempRealComponent = (realComponent * realComponent) - (imaginaryComponent * imaginaryComponent) + x
		imaginaryComponent = 2*realComponent*imaginaryComponent + y
		realComponent = tempRealComponent

		if realComponent*imaginaryComponent > 5 {
			colorHSV := colorful.Hsv(i*360/m.MaxIterations, 0.98, 0.922) // hue bar color (Hsv)
			return uint8(colorHSV.R*255), uint8(colorHSV.G*255), uint8(colorHSV.B*255)
		}
	}

	return 0, 0, 0 //black
}

func (m *Mandelbrot) Draw() {
	rl.BeginDrawing()
	rl.ClearBackground(rl.Black)
	rl.BeginTextureMode(m.Canvas)
	for x := int32(0); x < m.ScreenWidth; x++ {
		for y := int32(0); y < m.ScreenHeight; y++ {
			rl.DrawPixel(x, y, m.Pixels[x][y])
		}
	}
	rl.EndTextureMode()
	rl.DrawTexture(m.Canvas.Texture, 0, 0, rl.RayWhite)

	raygui.SetStyleProperty(raygui.GlobalTextFontsize, 20.0)
	raygui.SetStyleProperty(raygui.LabelTextColor, 16448200)

	label_height := 20
	for thread_index := 0; thread_index < len(m.ThreadsProcessTimes); thread_index++ {
		raygui.Label(rl.NewRectangle(30, float32(10+thread_index*(label_height+10)), 200, float32(label_height)), fmt.Sprintf("(Thread: %d) (time: %s)\n", thread_index, m.ThreadsProcessTimes[thread_index]))
	}

	raygui.Label(rl.NewRectangle(30, float32(10+len(m.ThreadsProcessTimes)*(label_height+10)), 200, float32(label_height)), fmt.Sprintf("(Process time: %s)\n", m.TotalProcessTime))
	raygui.Label(rl.NewRectangle(30, float32(10+(len(m.ThreadsProcessTimes)+1)*(label_height+10)), 200, float32(label_height)), fmt.Sprintf("(FPS: %f)\n", rl.GetFPS()))

	rl.EndDrawing()
}

func (m *Mandelbrot) ProcessKeyboard() {
	m.NeedUpdate = false
	if rl.IsKeyDown(rl.KeyLeft) {
		m.PanX -= m.MovementOffset[int(m.ZoomLevel)]
		m.NeedUpdate = true
	}

	if rl.IsKeyDown(rl.KeyRight) {
		m.PanX += m.MovementOffset[int(m.ZoomLevel)]
		m.NeedUpdate = true
	}

	if rl.IsKeyDown(rl.KeyUp) {
		m.PanY += m.MovementOffset[int(m.ZoomLevel)]
		m.NeedUpdate = true
	}

	if rl.IsKeyDown(rl.KeyDown) {
		m.PanY -= m.MovementOffset[int(m.ZoomLevel)]
		m.NeedUpdate = true
	}

	if rl.IsKeyDown(rl.KeyA) {
		m.ZoomLevel += 0.01
		m.MagnificationFactor = 400 + math.Exp2(m.ZoomLevel*3)
		m.MaxIterations = 80 + 50*m.ZoomLevel
		m.NeedUpdate = true
	}

	if rl.IsKeyDown(rl.KeyS) {
		m.ZoomLevel -= 0.01
		m.MagnificationFactor = 400 + math.Exp2(m.ZoomLevel*3)
		m.MaxIterations = 80 + 50*m.ZoomLevel
		m.NeedUpdate = true
	}
}

func (m *Mandelbrot) ProcessRequestsFromMasterNode() {
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", m.SlavePort))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	fmt.Println("\nListening for Mandelbrot jobs at 0.0.0.0 on port", m.SlavePort)
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	slaveNodeServer := MandelbrotSlaveNodeServer{Mandelbrot: *m}
	proto.RegisterMandelbrotSlaveNodeServer(grpcServer, &slaveNodeServer)
	grpcServer.Serve(lis)
}

// MandelbrotSlaveNodeServer functions

type MandelbrotSlaveNodeServer struct {
	proto.UnimplementedMandelbrotSlaveNodeServer
	Mandelbrot Mandelbrot
}

func (s *MandelbrotSlaveNodeServer) CalculateRegion(ctx context.Context, request *proto.CalculateRegionRequest) (*proto.CalculateRegionResponse, error) {

	s.Mandelbrot.MagnificationFactor = request.GetMagnificationFactor()
	s.Mandelbrot.MaxIterations = request.GetMaxIterations()
	s.Mandelbrot.PanX = request.GetPanX()
	s.Mandelbrot.PanY = request.GetPanY()
	s.Mandelbrot.RegionWidth = request.GetRegionWidth()
	s.Mandelbrot.RegionHeight = request.GetRegionHeight()
	regionIndex := request.GetRegionIndex()

	if s.Mandelbrot.RGBBuffer == nil {
		// Allocate memory for the rgb-pixel buffer used as response
		s.Mandelbrot.RGBBuffer = make([]byte, s.Mandelbrot.RegionWidth*s.Mandelbrot.RegionHeight*3)
	}

	s.Mandelbrot.CalculateRegionLocally(regionIndex*s.Mandelbrot.RegionWidth, 0, regionIndex*s.Mandelbrot.RegionWidth+s.Mandelbrot.RegionWidth, s.Mandelbrot.RegionHeight)

	return &proto.CalculateRegionResponse{RGBPixels: s.Mandelbrot.RGBBuffer}, nil
}

// Other functions

func MIN(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func GetClosestDivisibleNumber(n int, m int) int {
	q := n / m
	n1 := m * q
	var n2 int
	if (n * m) > 0 {
		n2 = (m * (q + 1))
	} else {
		n2 = (m * (q - 1))
	}

	if (math.Abs(float64(n - n1)) < math.Abs(float64(n - n2))) {
		return n1
	}

	return n2
}
