package service

import (
	"fmt"
	"log"
	"sync"
	"time"

	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/openai"
	"report-analyze-pipeline/parser"
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
	reports, err := s.db.GetUnanalyzedReports(s.config, 10) // Process up to 10 reports at a time
	if err != nil {
		log.Printf("Failed to get unanalyzed reports: %v", err)
		return
	}

	if len(reports) == 0 {
		return
	}

	log.Printf("Processing %d unanalyzed reports", len(reports))

	wg := sync.WaitGroup{}
	wg.Add(len(reports))
	for _, report := range reports {
		go s.analyzeReport(&report, &wg)
	}
	wg.Wait()
}

// analyzeReport analyzes a single report
func (s *Service) analyzeReport(report *database.Report, wg *sync.WaitGroup) {
	defer wg.Done()

	// Prepare the prompt for OpenAI
	prompt := fmt.Sprintf(`
%s

Analyze this image and provide a JSON response with the following structure:
{
  "title": "A descriptive title for the issue",
  "description": "A detailed description of what you see in the image",
  "litter_probability": 0.0-1.0,
  "hazard_probability": 0.0-1.0,
  "severity_level": 0.0-1.0
}
`, s.config.AnalysisPrompt)

	// Call OpenAI API
	response, err := s.openai.AnalyzeImage(report.Image, prompt)
	if err != nil {
		log.Printf("Failed to analyze report %d: %v", report.Seq, err)
		return
	}

	// Parse the response
	analysis, err := parser.ParseAnalysis(response)
	if err != nil {
		log.Printf("Failed to parse analysis for report %d: %v", report.Seq, err)
		return
	}

	// Create the analysis result
	analysisResult := &database.ReportAnalysis{
		Seq:               report.Seq,
		Source:            "ChatGPT",
		AnalysisText:      response,
		AnalysisImage:     nil, // OpenAI doesn't return images in this context
		Title:             analysis.Title,
		Description:       analysis.Description,
		LitterProbability: analysis.LitterProbability,
		HazardProbability: analysis.HazardProbability,
		SeverityLevel:     analysis.SeverityLevel,
		Summary:           analysis.Title + ": " + analysis.Description,
	}

	// Save the analysis to the database
	if err := s.db.SaveAnalysis(analysisResult); err != nil {
		log.Printf("Failed to save analysis for report %d: %v", report.Seq, err)
	}
}
