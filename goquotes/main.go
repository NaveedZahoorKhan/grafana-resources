package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	maxRequestsPerMinute = 90
	requestInterval      = time.Minute / maxRequestsPerMinute
)

type Quote struct {
	ID           string   `json:"_id"`
	Author       string   `json:"author"`
	Content      string   `json:"content"`
	Tags         []string `json:"tags"`
	AuthorSlug   string   `json:"authorSlug"`
	Length       int      `json:"length"`
	DateAdded    string   `json:"dateAdded"`
	DateModified string   `json:"dateModified"`
}

var (
	totalRequests = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "total_requests",
			Help: "Total number of requests made",
		},
	)
	requestLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "request_latency_seconds",
			Help: "Request latency in seconds",
		},
	)
)

func init() {
	prometheus.MustRegister(totalRequests)
	prometheus.MustRegister(requestLatency)
}
func main() {
	// Create a channel to control the rate of requests
	requestLimiter := time.Tick(requestInterval)

	// Create a wait group to wait for all workers to finish
	var wg sync.WaitGroup

	// Create a buffered channel for storing quotes
	quotesCh := make(chan Quote, maxRequestsPerMinute)

	// Start worker goroutines
	for i := 0; i < maxRequestsPerMinute; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				<-requestLimiter // Wait for the request interval
				start := time.Now()
				quote, err := fetchRandomQuote()
				if err != nil {
					fmt.Println("Error fetching quote:", err)
					return
				}
				requestLatency.Observe(time.Since(start).Seconds()) // Measure request latency
				totalRequests.Inc()                                 // Increment the total requests counter
				quotesCh <- quote
			}
		}()
	}

	http.Handle("/metrics", promhttp.Handler())
	go func() {
		http.ListenAndServe(":8080", nil)
	}()

	// Create a goroutine to close the quotes channel and wait for all workers
	go func() {
		wg.Wait()
		close(quotesCh)
	}()

	// Process and print quotes from the channel
	for quote := range quotesCh {
		fmt.Printf("Author: %s\nQuote: %s\nTags: %v\n\n", quote.Author, quote.Content, quote.Tags)
	}

}

func fetchRandomQuote() (Quote, error) {
	response, err := http.Get("https://api.quotable.io/random")
	if err != nil {
		return Quote{}, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return Quote{}, err
	}

	var quote Quote
	if err := json.Unmarshal(body, &quote); err != nil {
		return Quote{}, err
	}

	return quote, nil
}
