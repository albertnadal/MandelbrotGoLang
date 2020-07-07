package main

import (
	"os"
	"flag"
	"fmt"
	"runtime/pprof"
	"github.com/gen2brain/raylib-go/raygui"
	"github.com/gen2brain/raylib-go/raylib"
	"github.com/lucasb-eyer/go-colorful"
	"log"
	"math"
	"runtime"
	"sync"
	"time"
)

const DEBUG bool = false
const PROFILE bool = false

const MAX_THREADS int32 = 16
const SCREEN_WIDTH int32 = 1280 / 3
const SCREEN_HEIGHT int32 = 720 / 3

type Mandelbrot struct {
	ScreenWidth         int32
	ScreenHeight        int32
	Pixels              [][]rl.Color
	MagnificationFactor float64
	MaxIterations       float64
	PanX                float64
	PanY                float64
	WaitGroup           sync.WaitGroup
	NeedUpdate          bool
	MaxThreads          int32
	ThreadsProcessTimes []time.Duration
	TotalProcessTime    time.Duration
	ZoomLevel           float64
	Canvas              rl.RenderTexture2D
	MovementOffset      [16]float64
}

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")
var memprofile = flag.String("memprofile", "", "write memory profile to `file`")

func main() {

	if PROFILE {
		flag.Parse()
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
	}

	// Ask the Golang runtime how many CPU cores are available
	totalCores := runtime.NumCPU()
	fmt.Printf("- Total Multi-threaded Cores available: %d\n", totalCores)
	fmt.Printf("- Using %d Cores\n", totalCores)
	// Set-up the Go runtime to use all the available CPU cores
	runtime.GOMAXPROCS(totalCores)

	rl.InitWindow(SCREEN_WIDTH, SCREEN_HEIGHT, "Mandelbrot fractal")
	rl.SetTargetFPS(30)

	fractal := Mandelbrot{}
	fractal.Init()

	fmt.Printf("\n- Use keys A and S for zoom-in and zoom-out.\n")
	fmt.Printf("- Use arrow keys to navigate.\n\n")

	for !rl.WindowShouldClose() {
		fractal.Update()
		fractal.Draw()
		fractal.ProcessKeyboard()
	}

	rl.UnloadTexture(fractal.Canvas.Texture)
	rl.CloseWindow()

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

}

func (m *Mandelbrot) Update() {
	if !m.NeedUpdate {
		return
	}

	areaWidth := (m.ScreenWidth - 1) / m.MaxThreads
	areaHeight := m.ScreenHeight - 1

	start := time.Now()
	for i := int32(0); i < m.MaxThreads; i++ {
		m.WaitGroup.Add(1)
		go m.UpdateArea(i, i*areaWidth+1, 0, i*areaWidth+areaWidth+i, areaHeight)
	}
	m.WaitGroup.Wait()
	m.TotalProcessTime = time.Since(start)
}

func (m *Mandelbrot) UpdateArea(thread_index int32, x_start int32, y_start int32, x_end int32, y_end int32) {
	defer m.WaitGroup.Done()

	start := time.Now()
	for x := x_start; (x <= x_end) && (x < m.ScreenWidth); x++ {
		for y := y_start; y <= y_end; y++ {
			calc_start := time.Now()
			m.Pixels[x][y] = m.getPixelColorAtPosition((float64(x)/m.MagnificationFactor)-m.PanX, (float64(y)/m.MagnificationFactor)-m.PanY)

			if (DEBUG) && (x == 300) && (y == 300) {
				log.Printf("(%s)\n", time.Since(calc_start))
			}
		}
	}

	m.ThreadsProcessTimes[thread_index] = time.Since(start)
}

func (m *Mandelbrot) getPixelColorAtPosition(x float64, y float64) rl.Color {
	realComponent := x
	imaginaryComponent := y
	var tempRealComponent float64

	for i := float64(0); i < m.MaxIterations; i++ {
		tempRealComponent = (realComponent * realComponent) - (imaginaryComponent * imaginaryComponent) + x
		imaginaryComponent = 2*realComponent*imaginaryComponent + y
		realComponent = tempRealComponent

		if realComponent*imaginaryComponent > 5 {
			colorHSV := colorful.Hsv(i*360/m.MaxIterations, 0.98, 0.922) // hue bar color (Hsv)
			return rl.NewColor(uint8(colorHSV.R*255), uint8(colorHSV.G*255), uint8(colorHSV.B*255), 255)
		}
	}

	return rl.Black
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

func (m *Mandelbrot) Init() {
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
	m.Canvas = rl.LoadRenderTexture(m.ScreenWidth, m.ScreenHeight)

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

func MIN(a, b int) int {
	if a < b {
		return a
	}
	return b
}
