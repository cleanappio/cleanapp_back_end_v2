package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// payload mirrors the BulkIngestRequest schema.
type payload struct {
	Source string      `json:"source"`
	Items  []payloadIt `json:"items"`
}

type payloadIt struct {
	ExternalID string                 `json:"external_id"`
	Title      string                 `json:"title"`
	Content    string                 `json:"content"`
	URL        string                 `json:"url"`
	Score      float64                `json:"score"`
	Metadata   map[string]interface{} `json:"metadata"`
}

func main() {
	endpoint := flag.String("endpoint", os.Getenv("BULK_ENDPOINT"), "bulk ingest endpoint")
	token := flag.String("token", os.Getenv("BULK_TOKEN"), "Bearer token for fetcher auth")
	total := flag.Int("total", 100000, "number of fake reports to send")
	batchSize := flag.Int("batch", 1000, "items per request")
	source := flag.String("source", "load_test", "source field for payloads")
	flag.Parse()

	if *endpoint == "" {
		log.Fatal("endpoint required (use -endpoint or BULK_ENDPOINT env)")
	}

	batches := (*total + *batchSize - 1) / *batchSize
	client := &http.Client{Timeout: 60 * time.Second}

	var sent int64
	var inserted int64
	start := time.Now()

	var wg sync.WaitGroup
	for b := 0; b < batches; b++ {
		wg.Add(1)
		go func(batch int) {
			defer wg.Done()
			offset := batch * (*batchSize)
			size := *batchSize
			if offset+size > *total {
				size = *total - offset
			}
			if size <= 0 {
				return
			}

			p := payload{Source: *source, Items: make([]payloadIt, 0, size)}
			for i := 0; i < size; i++ {
				id := offset + i
				p.Items = append(p.Items, payloadIt{
					ExternalID: fmt.Sprintf("load-%d", id),
					Title:      fmt.Sprintf("Load Test Report %d", id),
					Content:    "example content",
					URL:        "https://example.com",
					Score:      rand.Float64(),
					Metadata: map[string]interface{}{
						"bulk_mode": true,
					},
				})
			}

			body, _ := json.Marshal(p)
			req, err := http.NewRequest(http.MethodPost, *endpoint, bytes.NewReader(body))
			if err != nil {
				log.Printf("batch %d: build request: %v", batch, err)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			if *token != "" {
				req.Header.Set("Authorization", "Bearer "+*token)
			}

			resp, err := client.Do(req)
			if err != nil {
				log.Printf("batch %d: request error: %v", batch, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				atomic.AddInt64(&inserted, int64(size))
			} else {
				log.Printf("batch %d: status %s", batch, resp.Status)
			}
			atomic.AddInt64(&sent, 1)
		}(b)
	}

	wg.Wait()
	duration := time.Since(start).Seconds()
	if duration == 0 {
		duration = 1
	}

	fmt.Printf("Sent %d batches, inserted ~%d reports in %.2fs (%.2f req/s, %.0f inserts/s)\n", sent, inserted, duration, float64(sent)/duration, float64(inserted)/duration)
}
