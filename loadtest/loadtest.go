package main

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	total := 5000
	concurrency := 200
	target := "http://localhost:8001/put"

	// Reuse connections — critical for high throughput
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = 300
	http.DefaultTransport.(*http.Transport).MaxIdleConns = 300

	latencies := make([]time.Duration, total)
	var failed atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	fmt.Printf("Starting load test: %d entries, concurrency=%d\n", total, concurrency)
	start := time.Now()

	for i := 0; i < total; i++ {
		wg.Add(1)
		sem <- struct{}{}

		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			data := fmt.Sprintf(
				`{"op":"PUT","key":"key%d","value":"val%d","idempotency_key":"load-%d"}`,
				i, i, i,
			)

			reqStart := time.Now()
			resp, err := http.Post(target, "application/json", bytes.NewBufferString(data))
			latencies[i] = time.Since(reqStart)

			if err != nil || resp.StatusCode != 200 {
				failed.Add(1)
			}
			if err == nil {
				resp.Body.Close()
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	p50 := latencies[int(math.Floor(float64(total)*0.50))]
	p95 := latencies[int(math.Floor(float64(total)*0.95))]
	p99 := latencies[int(math.Floor(float64(total)*0.99))]

	succeeded := int64(total) - failed.Load()

	fmt.Println("========== Load Test Results ==========")
	fmt.Printf("Total entries:   %d\n", total)
	fmt.Printf("Succeeded:       %d\n", succeeded)
	fmt.Printf("Failed:          %d\n", failed.Load())
	fmt.Printf("Duration:        %.2fs\n", duration.Seconds())
	fmt.Printf("Throughput:      %.0f entries/sec\n", float64(succeeded)/duration.Seconds())
	fmt.Printf("Latency p50:     %v\n", p50.Round(time.Millisecond))
	fmt.Printf("Latency p95:     %v\n", p95.Round(time.Millisecond))
	fmt.Printf("Latency p99:     %v\n", p99.Round(time.Millisecond))
	fmt.Println("=======================================")
}
