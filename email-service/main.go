package main

import (
	"flag"
	"log"
	"time"

	"email-service/config"
	"email-service/service"
)

var (
	pollInterval = flag.Duration("poll_interval", 30*time.Second, "Interval to poll for new reports")
)

func main() {
	flag.Parse()

	// Load configuration
	cfg := config.Load()

	// Create email service
	emailService, err := service.NewEmailService(cfg)
	if err != nil {
		log.Fatal("Failed to create email service:", err)
	}
	defer emailService.Close()

	log.Printf("Email service started. Polling every %v", *pollInterval)

	// Start polling for reports
	for {
		if err := emailService.ProcessReports(); err != nil {
			log.Printf("Error processing reports: %v", err)
		}
		time.Sleep(*pollInterval)
	}
}
