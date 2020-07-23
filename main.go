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
	"context"
	"flag"
	"fmt"
	"github.com/gen2brain/raylib-go/raygui"
	"github.com/gen2brain/raylib-go/raylib"
	"github.com/lucasb-eyer/go-colorful"
	"google.golang.org/grpc"
	"log"
	"mandelbrot-fractal/proto"
	"math"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"
)

const MAX_THREADS int32 = 16
const SCREEN_WIDTH int32 = 1280
const SCREEN_HEIGHT int32 = 720

type Mandelbrot struct {
	ScreenWidth              int32
	ScreenHeight             int32
	Pixels                   []rl.Color
	MagnificationFactor      float64
	MaxIterations            float64
	PanX                     float64
	PanY                     float64
	ThreadWaitGroup          sync.WaitGroup
	DistributedWaitGroup     sync.WaitGroup
	NeedUpdate               bool
	MaxLocalThreads          int32
	LocalThreadsProcessTimes []time.Duration
	FrameProcessTime         time.Duration
	ZoomLevel                float64
	Canvas                   rl.RenderTexture2D
	MovementOffset           [16]float64
	IsMaster                 bool
	SlavePort                int32
	SlavesIPs                []string
	SlavesClients            []proto.MandelbrotSlaveNodeClient // Used only in 'master' mode
	SlavesCount              int32
	NodesProcessTimes        []time.Duration   // Array of processing times of each slave node and the master node (last value in the array)
	NodesRegions             []NodeRegion      // Array of regions data assigned to each node
	NodesThreadsProcessTimes [][]time.Duration // Thread processing times of all slave nodes
	BalancedWorkloads        []int32           // Array of values within range [0-100] defining the workload of each slave and the master (last value)
	FragmentWidth            int32
	FragmentHeight           int32
	RGBBuffer                []byte
}

type NodeRegion struct {
	XStart int32
	XEnd   int32
	YStart int32
	YEnd   int32
	Width  int32
	Height int32
}

var nodeRole = flag.String("role", "master", "cluster node role: `master` or `slave`")
var slavesIPs = flag.String("slaves", "", "cluster node slaves IP's separated by comas")

func main() {
	flag.Parse()

	// Ask the Golang runtime how many CPU cores are available
	totalCores := runtime.NumCPU()
	isMaster := *nodeRole != "slave"
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
}

// Mandelbrot functions

func (m *Mandelbrot) Init(isMaster bool, slavesIPs []string) {
	m.ScreenWidth = SCREEN_WIDTH
	m.ScreenHeight = SCREEN_HEIGHT
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
	m.MaxLocalThreads = MAX_THREADS
	m.LocalThreadsProcessTimes = make([]time.Duration, m.MaxLocalThreads)
	m.FragmentWidth = int32(math.Ceil(float64(m.ScreenWidth-1) / float64(m.MaxLocalThreads)))
	m.FragmentHeight = m.ScreenHeight - 1
	m.SlavePort = 50051
	m.IsMaster = isMaster

	if m.IsMaster {
		m.Canvas = rl.LoadRenderTexture(m.ScreenWidth, m.ScreenHeight)
		m.SlavesCount = int32(len(slavesIPs))
		m.SlavesIPs = make([]string, m.SlavesCount)
		m.SlavesClients = make([]proto.MandelbrotSlaveNodeClient, m.SlavesCount)
		m.NodesProcessTimes = make([]time.Duration, m.SlavesCount+1)        // processing times for each each slave and the master (last value in array)
		m.NodesThreadsProcessTimes = make([][]time.Duration, m.SlavesCount) // thread processing times of all nodes in the cluster (slaves and master)

		// This array stores all slaves IPs
		for i := int32(0); i < m.SlavesCount; i++ {
			m.SlavesIPs[i] = slavesIPs[i]
		}

		// This array stores all thread processing times of all slave nodes
		for i := int32(0); i < m.SlavesCount; i++ {
			m.NodesThreadsProcessTimes[i] = make([]time.Duration, m.MaxLocalThreads)
		}

		m.BalancedWorkloads = make([]int32, m.SlavesCount+1) // balanced workloads for each slave and the master (last value in array)
		m.NodesRegions = make([]NodeRegion, m.SlavesCount+1) // balanced workloads for each slave and the master (last value in array)

		// Set initial relative workload values for each slave node and the master node
		portion_acc := int32(0)
		for d := int32(0); d < m.SlavesCount; d++ {
			workload_portion := 100 / float64(m.SlavesCount+1) //float64(m.SlavesCount + 1)
			if d%2 == 0 {
				m.BalancedWorkloads[d] = int32(math.Floor(workload_portion))
			} else {
				m.BalancedWorkloads[d] = int32(math.Ceil(workload_portion))
			}
			portion_acc += m.BalancedWorkloads[d]
		}
		m.BalancedWorkloads[m.SlavesCount] = int32(math.Abs(100 - float64(portion_acc))) // master worload

		// Initialize the gRPC client for each slave node
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

	// Initialize the pixel matrix
	m.Pixels = make([]rl.Color, m.ScreenWidth*m.ScreenHeight)
	for i := int32(0); i < int32(len(m.Pixels)); i++ {
		m.Pixels[i] = rl.NewColor(0, 0, 0, 255)
	}
}

func (m *Mandelbrot) Update() {
	if !m.NeedUpdate {
		return
	}

	start := time.Now()

	if m.SlavesCount == 0 {
		// SINGLE COMPUTER
		for i := int32(0); i < m.MaxLocalThreads; i++ {
			m.ThreadWaitGroup.Add(1)
			go m.CalculateFragmentInThread(i, i*m.FragmentWidth, 0, i*m.FragmentWidth+m.FragmentWidth, m.FragmentHeight-1, 0, m.ScreenWidth-1)
		}
		m.ThreadWaitGroup.Wait()

	} else {
		// DISTRIBUTED COMPUTING
		regionIndex := int32(0)

		// Upload workloads according to previous master and slaves processing times
		m.UpdateAndBalanceWorkload()

		// Calculate each region separatelly in a slave node identified by 'regionIndex'
		for regionIndex = 0; regionIndex < m.SlavesCount; regionIndex++ {
			node_region := m.NodesRegions[regionIndex]
			m.DistributedWaitGroup.Add(1)
			go m.CalculateRegionInSlaveNode(regionIndex, node_region.XStart, node_region.YStart, node_region.XEnd, node_region.YEnd)
		}

		// Calculate one region locally (master node)
		master_start := time.Now()
		node_region := m.NodesRegions[regionIndex]
		m.CalculateRegionLocally(node_region.XStart, node_region.YStart, node_region.XEnd, node_region.YEnd)
		m.NodesProcessTimes[regionIndex] = time.Since(master_start) // last item in NodesProcessTimes is used to save the process time of the master node

		// Wait for all distributed calculations
		m.DistributedWaitGroup.Wait()
	}

	m.FrameProcessTime = time.Since(start)
}

func (m *Mandelbrot) Draw() {
	rl.BeginDrawing()
	rl.ClearBackground(rl.Black)

	// Send updated texture from RAM to GPU
	rl.UpdateTexture(m.Canvas.Texture, m.Pixels)

	// Render texture in GPU to screen
	rl.DrawTexture(m.Canvas.Texture, 0, 0, rl.RayWhite)

	raygui.SetStyleProperty(raygui.GlobalTextFontsize, 14.0)
	raygui.SetStyleProperty(raygui.GlobalTextColor, 9999999)

	label_height := 14
	// Show master node threads processing times
	raygui.Label(rl.NewRectangle(0, 8, 40, float32(label_height)), fmt.Sprintf("MASTER\n"))
	for thread_index := 0; thread_index < len(m.LocalThreadsProcessTimes); thread_index++ {
		raygui.Label(rl.NewRectangle(0, float32(20+8+thread_index*(label_height+8)), 100, float32(label_height)), fmt.Sprintf("Thread %d: %s\n", thread_index, m.LocalThreadsProcessTimes[thread_index]))
	}

	// Show slave nodes threads processing times
	for region_index := 0; region_index < len(m.NodesThreadsProcessTimes); region_index++ {
		raygui.Label(rl.NewRectangle(float32(region_index+1)*160, 8, 40, float32(label_height)), fmt.Sprintf("NODE %d (%s)\n", region_index, m.SlavesIPs[region_index]))
		for thread_index := 0; thread_index < len(m.NodesThreadsProcessTimes[region_index]); thread_index++ {
			raygui.Label(rl.NewRectangle(float32(region_index+1)*160, float32(20+8+thread_index*(label_height+8)), 100, float32(label_height)), fmt.Sprintf("Thread %d: %s\n", thread_index, m.LocalThreadsProcessTimes[thread_index]))
		}
	}

	// Show frame total processing time and rendering FPS
	raygui.Label(rl.NewRectangle(0, float32(m.ScreenHeight-40), 100, float32(label_height)), fmt.Sprintf("(Frame time: %s)\n", m.FrameProcessTime))
	raygui.Label(rl.NewRectangle(0, float32(m.ScreenHeight-20), 100, float32(label_height)), fmt.Sprintf("(FPS: %f)\n", rl.GetFPS()))

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
		m.PanY -= m.MovementOffset[int(m.ZoomLevel)]
		m.NeedUpdate = true
	}

	if rl.IsKeyDown(rl.KeyDown) {
		m.PanY += m.MovementOffset[int(m.ZoomLevel)]
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

func (m *Mandelbrot) UpdateAndBalanceWorkload() {
	var minProcessTime, maxProcessTime time.Duration = 1 * time.Hour, 0
	var minProcessTimeRegionIndex, maxProcessTimeRegionIndex int32 = 0, 0

	// Search for the fastest and the slowest node
	for i := int32(0); i <= m.SlavesCount; i++ {
		if m.NodesProcessTimes[i] < minProcessTime {
			minProcessTime = m.NodesProcessTimes[i]
			minProcessTimeRegionIndex = i
		}

		if m.NodesProcessTimes[i] > maxProcessTime {
			maxProcessTime = m.NodesProcessTimes[i]
			maxProcessTimeRegionIndex = i
		}
	}

	// Balance the fastest and the slowest node
	if (m.BalancedWorkloads[minProcessTimeRegionIndex] < 100) && (m.BalancedWorkloads[maxProcessTimeRegionIndex] > 0) && (minProcessTimeRegionIndex != maxProcessTimeRegionIndex) {
		m.BalancedWorkloads[minProcessTimeRegionIndex]++
		m.BalancedWorkloads[maxProcessTimeRegionIndex]--
	}

	// Update node regions according to the new workloads calculated
	x := int32(0)
	for i := int32(0); i <= m.SlavesCount; i++ {
		workload := float64(m.BalancedWorkloads[i]) / 100
		m.NodesRegions[i].XStart = x
		m.NodesRegions[i].Width = int32(float64(m.ScreenWidth) * workload)
		x += m.NodesRegions[i].Width - 1
		m.NodesRegions[i].XEnd = x
		x++

		m.NodesRegions[i].YStart = 0
		m.NodesRegions[i].YEnd = m.ScreenHeight - 1
		m.NodesRegions[i].Height = m.ScreenHeight
	}
}

func (m *Mandelbrot) CalculateRegionInSlaveNode(region_index int32, x_start int32, y_start int32, x_end int32, y_end int32) {
	defer m.DistributedWaitGroup.Done()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()

	regionWidth := x_end - x_start + 1
	regionHeight := y_end - y_start + 1

	// Send the job to the slave node with the region to calculate
	response, err := m.SlavesClients[region_index].CalculateRegion(ctx, &proto.CalculateRegionRequest{MagnificationFactor: m.MagnificationFactor, MaxIterations: m.MaxIterations, PanX: m.PanX, PanY: m.PanY, Index: region_index, Width: regionWidth, Height: regionHeight, XStart: x_start, YStart: y_start, XEnd: x_end, YEnd: y_end})
	if err != nil {
		log.Fatalf("An error occurred when fetching data from slave node (%d) error: (%v)", region_index, err)
	}

	// Save the time spent by slave node to receive, process and return the region calculated
	m.NodesProcessTimes[region_index] = time.Since(start)

	// RGB buffer with calculated region values(pixels) in RGB
	rgbBuffer := response.GetRGBPixels()
	slaveThreadsProcessTimesInt64 := response.GetThreadsProcessTimes()

	// Update local buffer with the region calculated in a slave node
	var i int32 = 0
	for x := x_start; (x <= x_end) && (x < m.ScreenWidth); x++ {
		for y := y_start; y < y_end; y++ {
			// Update region pixels with the calculated values by the slave node
			m.Pixels[(m.ScreenWidth*y)+x] = rl.NewColor(rgbBuffer[i*3], rgbBuffer[i*3+1], rgbBuffer[i*3+2], 255) // RGBA
			i++
		}
	}

	// Store slave node threads processing times (used only to show node stats)
	for e := int32(0); e < m.MaxLocalThreads; e++ {
		m.NodesThreadsProcessTimes[region_index][e] = time.Duration(slaveThreadsProcessTimesInt64[e]) * time.Nanosecond
	}
}

func (m *Mandelbrot) CalculateRegionLocally(x_start int32, y_start int32, x_end int32, y_end int32) {
	regionWidth := x_end - x_start
	fragmentWidth := int32(math.Ceil(float64(regionWidth) / float64(m.MaxLocalThreads)))
	fragmentHeight := y_end - y_start

	for i := int32(0); i < m.MaxLocalThreads; i++ {
		m.ThreadWaitGroup.Add(1)
		go m.CalculateFragmentInThread(i, x_start+i*fragmentWidth, y_start, x_start+i*fragmentWidth+fragmentWidth, fragmentHeight, i*fragmentWidth*fragmentHeight, x_end)
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
			red, green, blue = m.GetPixelColorAtPosition((float64(x)/m.MagnificationFactor)-m.PanX, (float64(y)/m.MagnificationFactor)-m.PanY)
			if m.IsMaster {
				// RGBA buffer that will be sent to the GPU in order to draw the fractal in the screen
				m.Pixels[(m.ScreenWidth*y)+x] = rl.NewColor(red, green, blue, 255)
			} else {
				// RBG buffer used to store the data that should be sent to the master node
				m.RGBBuffer[offset*3+i*3] = red
				m.RGBBuffer[offset*3+i*3+1] = green
				m.RGBBuffer[offset*3+i*3+2] = blue
				i++
			}
		}
	}
	m.LocalThreadsProcessTimes[thread_index] = time.Since(start)
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
			return uint8(colorHSV.R * 255), uint8(colorHSV.G * 255), uint8(colorHSV.B * 255)
		}
	}

	return 0, 0, 0 //black
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
	regionWidth := request.GetWidth()
	regionHeight := request.GetHeight()
	regionXStart := request.GetXStart()
	regionXEnd := request.GetXEnd()
	regionYStart := request.GetYStart()
	regionYEnd := request.GetYEnd()

	// Following memory allocation is not efficient at all in terms of performance. Need some improvements.
	// Allocate memory for the rgb-pixel buffer used as response
	s.Mandelbrot.RGBBuffer = make([]byte, regionWidth*regionHeight*3)

	s.Mandelbrot.CalculateRegionLocally(regionXStart, regionYStart, regionXEnd, regionYEnd)

	localThreadsProcessTimesInt64 := make([]int64, s.Mandelbrot.MaxLocalThreads)
	for i := int32(0); i < s.Mandelbrot.MaxLocalThreads; i++ {
		localThreadsProcessTimesInt64[i] = s.Mandelbrot.LocalThreadsProcessTimes[i].Nanoseconds()
	}

	return &proto.CalculateRegionResponse{RGBPixels: s.Mandelbrot.RGBBuffer, ThreadsProcessTimes: localThreadsProcessTimesInt64}, nil
}

// Other functions

func MIN(a, b int) int {
	if a < b {
		return a
	}
	return b
}
