package service

import (
	"log"
	"sync"
	"time"

	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/openai"
	"report-analyze-pipeline/parser"
	"report-analyze-pipeline/services"
)

// Service represents the report analysis service
type Service struct {
	config          *config.Config
	db              *database.Database
	openai          *openai.Client
	openaiAssistant *openai.AssistantClient
	brandService    *services.BrandService
	stopChan        chan bool
}

// NewService creates a new report analysis service
func NewService(cfg *config.Config, db *database.Database) *Service {
	client := openai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIModel)
	openaiAssistant := openai.NewAssistantClient(cfg.OpenAIAPIKey, cfg.OpenAIAssistantID)
	brandService := services.NewBrandService()

	return &Service{
		config:          cfg,
		db:              db,
		openai:          client,
		openaiAssistant: openaiAssistant,
		brandService:    brandService,
		stopChan:        make(chan bool),
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

	// Migrate the report_analysis table if it doesn't exist
	if err := s.db.MigrateReportAnalysisTable(); err != nil {
		log.Printf("Failed to migrate report_analysis table: %v", err)
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

	// Call OpenAI API with assistant for initial analysis in English
	response, err := s.openaiAssistant.AnalyseImageWithAssistant(report.Image)
	if err != nil {
		log.Printf("Failed to analyze report %d: %v", report.Seq, err)
		// Save error report
		errorAnalysis := &database.ReportAnalysis{
			Seq:            report.Seq,
			Source:         "ChatGPT",
			IsValid:        false,
			Classification: "physical",
		}
		if saveErr := s.db.SaveAnalysis(errorAnalysis); saveErr != nil {
			log.Printf("Failed to save error analysis for report %d: %v", report.Seq, saveErr)
		} else {
			log.Printf("Saved error analysis for report %d (analysis failed)", report.Seq)
		}
		return
	}

	// Parse the response
	analysis, err := parser.ParseAnalysis(response)
	if err != nil {
		log.Printf("Failed to parse analysis for report %d: %v", report.Seq, err)
		// Save error report
		errorAnalysis := &database.ReportAnalysis{
			Seq:            report.Seq,
			Source:         "ChatGPT",
			IsValid:        false,
			Classification: "physical",
		}
		if saveErr := s.db.SaveAnalysis(errorAnalysis); saveErr != nil {
			log.Printf("Failed to save error analysis for report %d: %v", report.Seq, saveErr)
		} else {
			log.Printf("Saved error analysis for report %d (parsing failed)", report.Seq)
		}
		return
	}

	// Normalize the brand name before saving
	normalizedBrandName := s.brandService.NormalizeBrandName(analysis.BrandName)
	brandDisplayName := analysis.BrandName

	// Create the English analysis result
	analysisResult := &database.ReportAnalysis{
		Seq:                   report.Seq,
		Source:                "ChatGPT",
		AnalysisText:          response,
		AnalysisImage:         nil, // OpenAI doesn't return images in this context
		Title:                 analysis.Title,
		Description:           analysis.Description,
		BrandName:             normalizedBrandName,
		BrandDisplayName:      brandDisplayName,
		LitterProbability:     analysis.LitterProbability,
		HazardProbability:     analysis.HazardProbability,
		DigitalBugProbability: analysis.DigitalBugProbability,
		SeverityLevel:         analysis.SeverityLevel,
		Summary:               analysis.Title + ": " + analysis.Description,
		Language:              "en",
		IsValid:               analysis.IsValid,
		Classification:        analysis.Classification.String(),
	}

	// Save the English analysis to the database
	if err := s.db.SaveAnalysis(analysisResult); err != nil {
		log.Printf("Failed to save English analysis for report %d: %v", report.Seq, err)
		return
	} else {
		log.Printf("Successfully saved English analysis for report %d", report.Seq)
	}

	// Asynchronous translations
	var transWg sync.WaitGroup
	for code, fullName := range s.config.TranslationLanguages {
		if code == "en" || fullName == "English" {
			continue // Skip English as we already have it
		}

		transWg.Add(1)
		langCode := code
		langName := fullName
		log.Printf("Translating to %s", langName)
		go func() {
			defer transWg.Done()
			// Translate the analysis text using the full language name
			translatedText, err := s.openai.TranslateAnalysis(string(response), langName)
			if err != nil {
				log.Printf("Failed to translate analysis for report %d to %s: %v", report.Seq, langName, err)
				return
			}

			// Parse the translated response
			translatedAnalysis, err := parser.ParseAnalysis(translatedText)
			if err != nil {
				log.Printf("Failed to parse translated analysis for report %d in %s: %v", report.Seq, langName, err)
				return
			}
			// Normalize the brand name for translated analysis
			normalizedTranslatedBrandName := s.brandService.NormalizeBrandName(translatedAnalysis.BrandName)
			translatedBrandDisplayName := translatedAnalysis.BrandName

			// Create the translated analysis result
			translatedResult := &database.ReportAnalysis{
				Seq:                   report.Seq,
				Source:                "ChatGPT",
				AnalysisText:          translatedText,
				AnalysisImage:         nil,
				Title:                 translatedAnalysis.Title,
				Description:           translatedAnalysis.Description,
				BrandName:             normalizedTranslatedBrandName,
				BrandDisplayName:      translatedBrandDisplayName,
				LitterProbability:     translatedAnalysis.LitterProbability,
				HazardProbability:     translatedAnalysis.HazardProbability,
				DigitalBugProbability: translatedAnalysis.DigitalBugProbability,
				SeverityLevel:         translatedAnalysis.SeverityLevel,
				Summary:               translatedAnalysis.Title + ": " + translatedAnalysis.Description,
				Language:              langCode, // Store the language code in the database
				IsValid:               translatedAnalysis.IsValid,
				Classification:        translatedAnalysis.Classification.String(),
			}

			// Save the translated analysis to the database
			if err := s.db.SaveAnalysis(translatedResult); err != nil {
				log.Printf("Failed to save %s analysis for report %d: %v", langName, report.Seq, err)
			} else {
				log.Printf("Successfully saved %s analysis for report %d", langName, report.Seq)
			}
		}()
	}
	transWg.Wait()
}
