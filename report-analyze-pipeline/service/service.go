package service

import (
	"log"
	"time"

	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/openai"
)

// Service represents the report analysis service
type Service struct {
	config   *config.Config
	db       *database.Database
	openai   *openai.Client
	stopChan chan bool
}

// NewService creates a new report analysis service
func NewService(cfg *config.Config, db *database.Database) *Service {
	client := openai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIModel)

	return &Service{
		config:   cfg,
		db:       db,
		openai:   client,
		stopChan: make(chan bool),
	}
}

// Start starts the analysis service
func (s *Service) Start() {
	log.Println("Starting report analysis service...")

	// Create the report_analysis table if it doesn't exist
	if err := s.db.CreateReportAnalysisTable(); err != nil {
		log.Printf("Failed to create report_analysis table: %v", err)
		return
	}

	// Start the analysis loop
	go s.analysisLoop()
}

// Stop stops the analysis service
func (s *Service) Stop() {
	log.Println("Stopping report analysis service...")
	close(s.stopChan)
}

// analysisLoop continuously processes unanalyzed reports
func (s *Service) analysisLoop() {
	ticker := time.NewTicker(s.config.AnalysisInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			log.Println("Analysis loop stopped")
			return
		case <-ticker.C:
			s.processUnanalyzedReports()
		}
	}
}

// processUnanalyzedReports processes reports that haven't been analyzed yet
func (s *Service) processUnanalyzedReports() {
	reports, err := s.db.GetUnanalyzedReports(10) // Process up to 10 reports at a time
	if err != nil {
		log.Printf("Failed to get unanalyzed reports: %v", err)
		return
	}

	if len(reports) == 0 {
		log.Println("No unanalyzed reports found")
		return
	}

	log.Printf("Processing %d unanalyzed reports", len(reports))

	for _, report := range reports {
		if err := s.analyzeReport(&report); err != nil {
			log.Printf("Failed to analyze report %d: %v", report.Seq, err)
			continue
		}
		log.Printf("Successfully analyzed report %d", report.Seq)
	}
}

// analyzeReport analyzes a single report
func (s *Service) analyzeReport(report *database.Report) error {
	// Use the configurable analysis prompt
	prompt := s.config.AnalysisPrompt

	// Analyze the image using OpenAI
	analysisText, err := s.openai.AnalyzeImage(report.Image, prompt)
	if err != nil {
		return err
	}

	// Create the analysis result
	analysis := &database.ReportAnalysis{
		Seq:           report.Seq,
		Source:        "ChatGPT",
		AnalysisText:  analysisText,
		AnalysisImage: nil, // OpenAI doesn't return images in this context
	}

	// Save the analysis to the database
	return s.db.SaveAnalysis(analysis)
}
