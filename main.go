package main

import (
	"fmt"
	"github.com/gen2brain/raylib-go/raygui"
	"github.com/gen2brain/raylib-go/raylib"
	"math"
	"runtime"
	"sync"
	"time"
)

const MAX_THREADS int32 = 16

type Mandelbrot struct {
	ScreenWidth         int32
	ScreenHeight        int32
	Pixels              [][]rl.Color
	MagnificationFactor float64
	MaxIterations       uint32
	PanX                float64
	PanY                float64
	WaitGroup           sync.WaitGroup
	NeedUpdate          bool
	MaxThreads          int32
	ThreadsProcessTimes []time.Duration
	TotalProcessTime    time.Duration
	ZoomLevel           float64
}

func main() {

	// Ask the Golang runtime how many CPU cores are available
	totalCores := runtime.NumCPU()
	fmt.Printf("Total Multi-threaded Cores available: %d\n", totalCores)
	fmt.Printf("Using %d Cores\n", totalCores)
	// Set-up the Go runtime to use all the available CPU cores
	runtime.GOMAXPROCS(totalCores)

	fractal := Mandelbrot{}
	fractal.Init()

	rl.InitWindow(fractal.ScreenWidth, fractal.ScreenHeight, "Mandelbrot fractal")
	rl.SetTargetFPS(30)

	for !rl.WindowShouldClose() {
		fractal.Update()
		fractal.Draw()
		fractal.ProcessKeyboard()
	}

	rl.CloseWindow()
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
		go m.UpdateArea(i, i*areaWidth, 0, i*areaWidth+areaWidth, areaHeight)
	}
	m.WaitGroup.Wait()
	m.TotalProcessTime = time.Since(start)
}

func (m *Mandelbrot) UpdateArea(thread_index int32, x_start int32, y_start int32, x_end int32, y_end int32) {
	defer m.WaitGroup.Done()

	start := time.Now()
	for x := int32(x_start); x <= x_end; x++ {
		for y := int32(y_start); y <= y_end; y++ {
			m.Pixels[x][y] = m.getPixelColorAtPosition((float64(x)/m.MagnificationFactor)-m.PanX, (float64(y)/m.MagnificationFactor)-m.PanY)
		}
	}

	m.ThreadsProcessTimes[thread_index] = time.Since(start)
}

func (m *Mandelbrot) getPixelColorAtPosition(x float64, y float64) rl.Color {
	realComponent := x
	imaginaryComponent := y
	tempRealComponent := float64(0.0)
	tempImaginaryComponent := float64(0.0)

	for i := uint32(0); i < m.MaxIterations; i++ {
		tempRealComponent = (realComponent * realComponent) - (imaginaryComponent * imaginaryComponent) + x
		tempImaginaryComponent = 2.0*realComponent*imaginaryComponent + y
		realComponent = tempRealComponent
		imaginaryComponent = tempImaginaryComponent

		if realComponent*imaginaryComponent > 5 {
			return rl.NewColor(uint8((i*255)/m.MaxIterations), 0, 0, 255)
		}
	}

	return rl.Black
}

func (m *Mandelbrot) Draw() {
	rl.BeginDrawing()
	rl.ClearBackground(rl.Black)
	for x := int32(0); x < m.ScreenWidth; x++ {
		for y := int32(0); y < m.ScreenHeight; y++ {
			rl.DrawPixel(x, y, m.Pixels[x][y])
		}
	}

	raygui.SetStyleProperty(raygui.GlobalTextFontsize, 20.0)
	raygui.SetStyleProperty(raygui.LabelTextColor, 16448250)

	label_height := 20
	for thread_index := 0; thread_index < len(m.ThreadsProcessTimes); thread_index++ {
		raygui.Label(rl.NewRectangle(30, float32(10+thread_index*(label_height+10)), 200, float32(label_height)), fmt.Sprintf("(Thread: %d) (time: %s)\n", thread_index, m.ThreadsProcessTimes[thread_index]))
	}

	raygui.Label(rl.NewRectangle(30, float32(10+len(m.ThreadsProcessTimes)*(label_height+10)), 200, float32(label_height)), fmt.Sprintf("(Total time: %s)\n", m.TotalProcessTime))

	rl.EndDrawing()
}

func (m *Mandelbrot) ProcessKeyboard() {
	m.NeedUpdate = false
	if rl.IsKeyDown(rl.KeyLeft) {
		m.PanX -= 0.5 / math.Exp2(m.ZoomLevel/1.65)
		m.NeedUpdate = true
	}

	if rl.IsKeyDown(rl.KeyRight) {
		m.PanX += 0.5 / math.Exp2(m.ZoomLevel/1.65)
		m.NeedUpdate = true
	}

	if rl.IsKeyDown(rl.KeyUp) {
		m.PanY -= 1.0 / math.Exp2(m.ZoomLevel/1.65)
		m.NeedUpdate = true
	}

	if rl.IsKeyDown(rl.KeyDown) {
		m.PanY += 1.0 / math.Exp2(m.ZoomLevel/1.65)
		m.NeedUpdate = true
	}

	if rl.IsKeyDown(rl.KeyA) {
		m.ZoomLevel += 0.1
		m.MagnificationFactor = 400 + math.Exp2(m.ZoomLevel)
    m.MaxIterations = 50 + uint32(15.5*m.ZoomLevel)
		m.NeedUpdate = true
	}

	if rl.IsKeyDown(rl.KeyS) {
		m.ZoomLevel -= 0.1
		m.MagnificationFactor = 400 + math.Exp2(m.ZoomLevel)
    m.MaxIterations = 50 + uint32(15.5*m.ZoomLevel)
		m.NeedUpdate = true
	}
}

func (m *Mandelbrot) Init() {
	m.ScreenWidth = 1280
	m.ScreenHeight = 720
	m.MagnificationFactor = 400.0
	m.MaxIterations = 50
	m.PanX = 2.0
	m.PanY = 1.0
	m.NeedUpdate = true
	m.ZoomLevel = 1
	m.MaxThreads = MAX_THREADS
	m.ThreadsProcessTimes = make([]time.Duration, m.MaxThreads)

	m.Pixels = make([][]rl.Color, m.ScreenWidth)
	for i := int32(0); i < m.ScreenWidth; i++ {
		m.Pixels[i] = make([]rl.Color, m.ScreenHeight)
	}

	for x := int32(0); x < m.ScreenWidth; x++ {
		for y := int32(0); y < m.ScreenHeight; y++ {
			m.Pixels[x][y] = rl.NewColor(255, 0, 0, 255)
		}
	}
}

func MIN(a, b int) int {
	if a < b {
		return a
	}
	return b
}
