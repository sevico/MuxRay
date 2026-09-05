package main

type benchmarkClock struct {
	read      func() (int64, error)
	frequency float64
	source    string
}

type clockReading struct {
	ticks int64
	err   error
}
