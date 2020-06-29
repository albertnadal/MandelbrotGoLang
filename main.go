package main

import (
  "fmt"
  "time"
  "sync"
  "runtime"
  "github.com/gen2brain/raylib-go/raygui"
  "github.com/gen2brain/raylib-go/raylib"
)

type Mandelbrot struct {
   ScreenWidth         int32
   ScreenHeight        int32
   Pixels              [][]rl.Color
   MagnificationFactor float32
   MaxIterations       uint32
   PanX                float32
   PanY                float32
   WaitGroup           sync.WaitGroup
   NeedUpdate          bool
   TotalCPUsAvailable  int32
   CPUsProcessTimes    []time.Duration
   ProcessTime         time.Duration
}

func main() {
   fractal := Mandelbrot{}
   fractal.Init()

   rl.InitWindow(fractal.ScreenWidth, fractal.ScreenHeight, "Mandelbrot fractal")
   rl.SetTargetFPS(25)

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

   areaWidth := (m.ScreenWidth-1) / m.TotalCPUsAvailable
   areaHeight := m.ScreenHeight-1

   start := time.Now()
   for i := int32(0); i < m.TotalCPUsAvailable; i++ {
      m.WaitGroup.Add(1)
      go m.UpdateArea(i, i*areaWidth, 0, i*areaWidth + areaWidth, areaHeight)
   }
   m.WaitGroup.Wait()
   m.ProcessTime = time.Since(start)
   //fmt.Printf("Mandelbrot frame calculation (Total time: %s)\n", time.Since(start))
}

func (m *Mandelbrot) UpdateArea(cpu_index int32, x_start int32, y_start int32, x_end int32, y_end int32) {
   defer m.WaitGroup.Done()

   start := time.Now()
   for x := int32(x_start); x <= x_end; x++ {
      for y := int32(y_start); y <= y_end; y++ {
         m.Pixels[x][y] = m.getPixelColorAtPosition((float32(x) / m.MagnificationFactor) - m.PanX, (float32(y) / m.MagnificationFactor) - m.PanY)
      }
   }

   m.CPUsProcessTimes[cpu_index] = time.Since(start)
   //fmt.Printf("(CPU: %d) (%d,%d)(%d,%d) (Time spent: %s)\n", cpu_index, x_start, y_start, x_end, y_end, time.Since(start))
}

func (m *Mandelbrot) getPixelColorAtPosition(x float32, y float32) rl.Color {
   realComponent := x
   imaginaryComponent := y
   tempRealComponent := float32(0.0)
   tempImaginaryComponent := float32(0.0)

   for i := uint32(0); i < m.MaxIterations; i++ {
      tempRealComponent = (realComponent * realComponent) - (imaginaryComponent * imaginaryComponent) + x
      tempImaginaryComponent = 2.0 * realComponent * imaginaryComponent + y;
      realComponent = tempRealComponent
      imaginaryComponent = tempImaginaryComponent

      if (realComponent * imaginaryComponent > 5) {
         return rl.NewColor(uint8((i * 255) / m.MaxIterations), 0, 0, 255)
      }
   }

   return rl.Black
}

func (m *Mandelbrot) Draw() {
   rl.BeginDrawing()
   rl.ClearBackground(rl.Black)
   for x := int32(0); x < m.ScreenWidth; x++ {
      for y := int32(0); y < m.ScreenHeight; y++ {
         rl.DrawPixel(x, y, m.Pixels[x][y]);
      }
   }

   raygui.SetStyleProperty(raygui.GlobalTextFontsize, 20.0)
   raygui.SetStyleProperty(raygui.LabelTextColor, 16448250)

   for cpu_index := 0; cpu_index < len(m.CPUsProcessTimes); cpu_index++ {
      raygui.Label(rl.NewRectangle(30, float32(10 + cpu_index*(20 + 10)), 200, 20), fmt.Sprintf("(CPU: %d) (time: %s)\n", cpu_index, m.CPUsProcessTimes[cpu_index]))
   }

   raygui.Label(rl.NewRectangle(30, float32(10 + len(m.CPUsProcessTimes)*(20 + 10)), 200, 20), fmt.Sprintf("(Total time: %s)\n", m.ProcessTime))
/*
   raygui.Label(rl.NewRectangle(50, 10, 200, 20), fmt.Sprintf("(CPU: %d) (Time spent: %s)\n", cpu_index, time.Since(start)))
   m.PanX = raygui.Slider(rl.NewRectangle(50, 30, float32(m.ScreenWidth - 50 - 100), 20), m.PanX, 0, 5)
   raygui.Label(rl.NewRectangle(float32(m.ScreenWidth - 100 + 5), 30, 20, 20), fmt.Sprintf("%5.5f", m.PanX))

   raygui.Label(rl.NewRectangle(50, 70, 200, 20), "PanY")
   m.PanY = raygui.Slider(rl.NewRectangle(50, 90, float32(m.ScreenWidth - 50 - 100), 20), m.PanY, 0, 5)
   raygui.Label(rl.NewRectangle(float32(m.ScreenWidth - 100 + 5), 90, 20, 20), fmt.Sprintf("%5.5f", m.PanY))

   raygui.Label(rl.NewRectangle(50, 130, 200, 20), "Zoom")
   m.MagnificationFactor = raygui.Slider(rl.NewRectangle(50, 150, float32(m.ScreenWidth - 50 - 100), 20), m.MagnificationFactor, 0, 10000)
   raygui.Label(rl.NewRectangle(float32(m.ScreenWidth - 100 + 5), 150, 20, 20), fmt.Sprintf("%.0f", m.MagnificationFactor))
*/
   rl.EndDrawing()
}

func (m *Mandelbrot) ProcessKeyboard() {
  m.NeedUpdate = true//false
}

func (m *Mandelbrot) Init() {
   m.ScreenWidth = 1440
   m.ScreenHeight = 900
   m.MagnificationFactor = 400.0
   m.MaxIterations = 50
   m.PanX = 2.0
   m.PanY = 1.0
   m.NeedUpdate = true

   // Ask the Golang runtime how many CPU cores are available
   totalCPUs := runtime.NumCPU()
   fmt.Printf("Total Multi-threaded Cores available: %d\n", totalCPUs)
   // Set-up the Go runtime to use all the available CPU cores
   m.TotalCPUsAvailable = int32(runtime.GOMAXPROCS(totalCPUs))

   m.CPUsProcessTimes = make([]time.Duration, m.TotalCPUsAvailable)

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
