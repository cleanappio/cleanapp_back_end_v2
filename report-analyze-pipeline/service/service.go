package service

import (
	"log"
	"strings"
	"sync"
	"time"

	"report-analyze-pipeline/config"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/gemini"
	"report-analyze-pipeline/llm"
	"report-analyze-pipeline/models"
	"report-analyze-pipeline/openai"
	"report-analyze-pipeline/parser"
	"report-analyze-pipeline/rabbitmq"
	"report-analyze-pipeline/services"
)

// Service represents the report analysis service
type Service struct {
	config       *config.Config
	db           *database.Database
	llmClient    llm.Client
	brandService *services.BrandService
	publisher    *rabbitmq.Publisher
	stopChan     chan bool
}

// NewService creates a new report analysis service
func NewService(cfg *config.Config, db *database.Database) *Service {
	var client llm.Client
	if cfg.LLMProvider == "gemini" {
		client = gemini.NewClient(cfg.GeminiAPIKey, cfg.GeminiModel)
	} else {
		client = openai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIModel)
	}
	brandService := services.NewBrandService()
	// Log selected provider and model
	selectedModel := ""
	if cfg.LLMProvider == "gemini" {
		selectedModel = cfg.GeminiModel
	} else {
		selectedModel = cfg.OpenAIModel
	}
	log.Printf("Analyzer LLM provider=%s model=%s", client.SourceName(), selectedModel)

	// Initialize RabbitMQ publisher
	publisher, err := rabbitmq.NewPublisher(
		cfg.RabbitMQ.GetAMQPURL(),
		cfg.RabbitMQ.Exchange,
		cfg.RabbitMQ.AnalysedReportRoutingKey,
	)
	if err != nil {
		log.Printf("Failed to initialize RabbitMQ publisher: %v", err)
		// Continue without publisher - analysis will still work
		publisher = nil
	}

	return &Service{
		config:       cfg,
		db:           db,
		llmClient:    client,
		brandService: brandService,
		publisher:    publisher,
		stopChan:     make(chan bool),
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
}

// Stop stops the analysis service
func (s *Service) Stop() {
	log.Println("Stopping report analysis service...")

	// Close RabbitMQ publisher if it exists
	if s.publisher != nil {
		if err := s.publisher.Close(); err != nil {
			log.Printf("Failed to close RabbitMQ publisher: %v", err)
		}
	}

	close(s.stopChan)
}

// publishAnalyzedReport publishes a report with its analysis to RabbitMQ
func (s *Service) publishAnalyzedReport(report *database.Report, analyses []*database.ReportAnalysis) {
	if s.publisher == nil {
		log.Printf("RabbitMQ publisher not available, skipping publish for report %d", report.Seq)
		return
	}

	// Convert database models to API models
	apiReport := models.Report{
		Seq:         report.Seq,
		Timestamp:   report.Timestamp,
		ID:          report.ID,
		Team:        report.Team,
		Latitude:    report.Latitude,
		Longitude:   report.Longitude,
		X:           report.X,
		Y:           report.Y,
		ActionID:    report.ActionID,
		Description: report.Description,
	}

	var apiAnalyses []models.ReportAnalysis
	for _, analysis := range analyses {
		apiAnalysis := models.ReportAnalysis{
			Seq:                   analysis.Seq,
			Source:                analysis.Source,
			AnalysisText:          analysis.AnalysisText,
			Title:                 analysis.Title,
			Description:           analysis.Description,
			BrandName:             analysis.BrandName,
			BrandDisplayName:      analysis.BrandDisplayName,
			LitterProbability:     analysis.LitterProbability,
			HazardProbability:     analysis.HazardProbability,
			DigitalBugProbability: analysis.DigitalBugProbability,
			SeverityLevel:         analysis.SeverityLevel,
			Summary:               analysis.Summary,
			Language:              analysis.Language,
			Classification:        analysis.Classification,
			IsValid:               analysis.IsValid,
			InferredContactEmails: analysis.InferredContactEmails,
			CreatedAt:             time.Now(), // We don't have this in database model, use current time
			UpdatedAt:             time.Now(),
		}
		apiAnalyses = append(apiAnalyses, apiAnalysis)
	}

	// Create the report with analysis message
	reportWithAnalysis := models.ReportWithAnalysis{
		Report:   apiReport,
		Analysis: apiAnalyses,
	}

	// Publish to RabbitMQ
	if err := s.publisher.Publish(reportWithAnalysis); err != nil {
		log.Printf("Failed to publish analyzed report %d: %v", report.Seq, err)
	} else {
		log.Printf("Successfully published analyzed report %d with %d analyses", report.Seq, len(apiAnalyses))
	}
}

// analyzeReport analyzes a single report
func (s *Service) AnalyzeReport(report *database.Report) {
	// Collect all analyses for publishing
	var allAnalyses []*database.ReportAnalysis

	// Fetch only the image data from database
	imageData, err := s.db.GetReportImage(report.Seq)
	if err != nil {
		log.Printf("Failed to fetch image for report %d from database: %v", report.Seq, err)
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
			log.Printf("Saved error analysis for report %d (image fetch failed)", report.Seq)
		}
		// Add error analysis to collection and publish
		allAnalyses = append(allAnalyses, errorAnalysis)
		s.publishAnalyzedReport(report, allAnalyses)
		return
	}

	// Use the image from database and other fields from the report message
	log.Printf("Analyzing report %d with image size: %d bytes", report.Seq, len(imageData))

	// Call OpenAI API with assistant for initial analysis in English
	response, err := s.llmClient.AnalyzeImage(imageData, report.Description)
	if err != nil {
		log.Printf("Failed to analyze report %d: %v", report.Seq, err)
		// Save error report
		errorAnalysis := &database.ReportAnalysis{
			Seq:            report.Seq,
			Source:         s.llmClient.SourceName(),
			IsValid:        false,
			Classification: "physical",
		}
		if saveErr := s.db.SaveAnalysis(errorAnalysis); saveErr != nil {
			log.Printf("Failed to save error analysis for report %d: %v", report.Seq, saveErr)
		} else {
			log.Printf("Saved error analysis for report %d (analysis failed)", report.Seq)
		}
		// Add error analysis to collection and publish
		allAnalyses = append(allAnalyses, errorAnalysis)
		s.publishAnalyzedReport(report, allAnalyses)
		return
	}

	// Parse the response
	analysis, err := parser.ParseAnalysis(response)
	if err != nil {
		log.Printf("Failed to parse analysis for report %d: %v", report.Seq, err)
		// Save error report
		errorAnalysis := &database.ReportAnalysis{
			Seq:            report.Seq,
			Source:         s.llmClient.SourceName(),
			IsValid:        false,
			Classification: "physical",
		}
		if saveErr := s.db.SaveAnalysis(errorAnalysis); saveErr != nil {
			log.Printf("Failed to save error analysis for report %d: %v", report.Seq, saveErr)
		} else {
			log.Printf("Saved error analysis for report %d (parsing failed)", report.Seq)
		}
		// Add error analysis to collection and publish
		allAnalyses = append(allAnalyses, errorAnalysis)
		s.publishAnalyzedReport(report, allAnalyses)
		return
	}

	// Normalize the brand name before saving
	normalizedBrandName := s.brandService.NormalizeBrandName(analysis.BrandName)
	brandDisplayName := analysis.BrandName

	// Convert inferred contact emails to comma-separated string
	inferredContactEmails := strings.Join(analysis.InferredContactEmails, ", ")

	// Create the English analysis result
	analysisResult := &database.ReportAnalysis{
		Seq:                   report.Seq,
		Source:                s.llmClient.SourceName(),
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
		InferredContactEmails: inferredContactEmails,
	}

	// Save the English analysis to the database
	if err := s.db.SaveAnalysis(analysisResult); err != nil {
		log.Printf("Failed to save English analysis for report %d: %v", report.Seq, err)
		return
	} else {
		log.Printf("Successfully saved English analysis for report %d", report.Seq)
	}

	// Add English analysis to collection
	allAnalyses = append(allAnalyses, analysisResult)

	// Asynchronous translations
	var transWg sync.WaitGroup
	var analysesMutex sync.Mutex
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
			translatedText, err := s.llmClient.TranslateAnalysis(string(response), langName)
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

			// Convert inferred contact emails to comma-separated string
			inferredTranslatedContactEmails := strings.Join(translatedAnalysis.InferredContactEmails, ", ")

			// Create the translated analysis result
			translatedResult := &database.ReportAnalysis{
				Seq:                   report.Seq,
				Source:                s.llmClient.SourceName(),
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
				InferredContactEmails: inferredTranslatedContactEmails,
			}

			// Save the translated analysis to the database
			if err := s.db.SaveAnalysis(translatedResult); err != nil {
				log.Printf("Failed to save %s analysis for report %d: %v", langName, report.Seq, err)
			} else {
				log.Printf("Successfully saved %s analysis for report %d", langName, report.Seq)
				// Add translated analysis to collection safely
				analysesMutex.Lock()
				allAnalyses = append(allAnalyses, translatedResult)
				analysesMutex.Unlock()
			}
		}()
	}
	transWg.Wait()

	// Publish the analyzed report to RabbitMQ
	s.publishAnalyzedReport(report, allAnalyses)
}
