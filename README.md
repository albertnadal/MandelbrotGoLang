# Distributed computing of the Mandelbrot Set using Go, gRPC and Raylib

This is an implementation in Go of the Mandelbrot Set calculated taking profit of distributed computing and multithreading.

## Build and run on a single computer

```console
$ go run main.go
```

## Build and run on multiple computers (distributed computing)

Run the application in slave mode on a cluster node:

```console
$ go run main.go --role=slave
```

Run the application in master mode on a cluster node:

```console
$ go run main.go --role=master --slaves=192.16.0.2,192.16.0.3,192.16.0.4
```

192.16.0.2, 192.16.0.3 and 192.16.0.4 are sample IPs of cluster nodes with the application running in slave mode. The master node communicates continuously with the slave nodes and render all the regions of the Mandelbrot Set in real-time in a system window.

## Usage

Use **a** and **s** keys to zoom-in and zoom-out respectively (be patient when zooming). Use **arrow keys** to move.

## Screenshot

![Mandelbrot fractal](http://www.lafruitera.com/mandelbrot_golang.png)

## Demo

[![Mandelbrot Set](https://img.youtube.com/vi/pDbuClfEAIY/0.jpg)](https://www.youtube.com/watch?v=pDbuClfEAIY)
