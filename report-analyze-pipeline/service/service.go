package service

import (
	"log"
	"strings"
	"sync"
	"time"

	"report-analyze-pipeline/config"
	"report-analyze-pipeline/contacts"
	"report-analyze-pipeline/database"
	"report-analyze-pipeline/gemini"
	"report-analyze-pipeline/llm"
	"report-analyze-pipeline/models"
	"report-analyze-pipeline/openai"
	"report-analyze-pipeline/osm"
	"report-analyze-pipeline/parser"
	"report-analyze-pipeline/rabbitmq"
	"report-analyze-pipeline/services"
)

// Service represents the report analysis service
type Service struct {
	config          *config.Config
	db              *database.Database
	llmClient       llm.Client
	brandService    *services.BrandService
	osmService      *osm.CachedLocationService
	contactService  *contacts.ContactService
	publisher       *rabbitmq.Publisher
	stopChan        chan bool
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

	// Initialize OSM location service for physical report email inference
	osmService := osm.NewCachedLocationService(db.GetDB())

	// Initialize contact service for digital report email enrichment
	contactService := contacts.NewContactService(db.GetDB())

	return &Service{
		config:          cfg,
		db:              db,
		llmClient:       client,
		brandService:    brandService,
		osmService:      osmService,
		contactService:  contactService,
		publisher:       publisher,
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

	// Create OSM location cache table
	if err := s.osmService.CreateCacheTable(); err != nil {
		log.Printf("Failed to create osm_location_cache table: %v", err)
		// Continue - OSM is optional
	}

	// Create brand_contacts table and seed known contacts
	if err := s.contactService.CreateBrandContactsTable(); err != nil {
		log.Printf("Failed to create brand_contacts table: %v", err)
	} else {
		if err := s.contactService.SeedKnownContacts(); err != nil {
			log.Printf("Failed to seed known contacts: %v", err)
		}
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
			LegalRiskEstimate:     analysis.LegalRiskEstimate,
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
		LegalRiskEstimate:     analysis.LegalRiskEstimate,
	}

	// Save the English analysis to the database
	if err := s.db.SaveAnalysis(analysisResult); err != nil {
		log.Printf("Failed to save English analysis for report %d: %v", report.Seq, err)
		return
	} else {
		log.Printf("Successfully saved English analysis for report %d", report.Seq)
	}

	// Enrich digital reports SYNCHRONOUSLY before publishing to ensure notifications have contact data
	if analysisResult.Classification == "digital" {
		s.enrichDigitalReportEmails(report, analysisResult)
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
				LegalRiskEstimate:     translatedAnalysis.LegalRiskEstimate,
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

	// Background enrichment for physical reports only (digital handled synchronously above)
	if analysisResult.Classification == "physical" {
		go s.enrichPhysicalReportEmails(report, analysisResult)
	}
}

// enrichPhysicalReportEmails fetches OSM location context and enriches contact emails for physical reports
// This runs in a goroutine to avoid blocking the main analysis flow
func (s *Service) enrichPhysicalReportEmails(report *database.Report, analysis *database.ReportAnalysis) {
	// Skip if we already have good inferred emails (from image analysis)
	existingEmails := strings.Split(analysis.InferredContactEmails, ",")
	for i := range existingEmails {
		existingEmails[i] = strings.TrimSpace(existingEmails[i])
	}
	if len(osm.ValidateAndFilterEmails(existingEmails)) >= 3 {
		log.Printf("Report %d: Already has %d valid inferred emails, skipping OSM enrichment",
			report.Seq, len(existingEmails))
		return
	}

	// Check if we have cached emails for this location
	cachedEmails, err := s.osmService.GetCachedInferredEmails(report.Latitude, report.Longitude)
	if err == nil && cachedEmails != "" {
		log.Printf("Report %d: Using cached inferred emails for location (%.4f, %.4f)",
			report.Seq, report.Latitude, report.Longitude)
		if err := s.db.UpdateInferredContactEmails(report.Seq, cachedEmails); err != nil {
			log.Printf("Report %d: Failed to update with cached emails: %v", report.Seq, err)
		}
		return
	}

	// Collect all discovered emails with provenance
	var allEmails []string
	
	// Step 1: Reverse geocode with Nominatim
	locCtx, err := s.osmService.GetLocationContext(report.Latitude, report.Longitude)
	if err != nil {
		log.Printf("Report %d: Failed to get OSM location context: %v", report.Seq, err)
	}
	
	if locCtx != nil && locCtx.HasUsefulData() {
		log.Printf("Report %d: Nominatim returned: primary=%q, parent=%q, domain=%q, type=%q",
			report.Seq, locCtx.PrimaryName, locCtx.ParentOrg, locCtx.Domain, locCtx.LocationType)
		
		// Direct email from OSM tags (highest priority)
		if locCtx.ContactEmail != "" {
			allEmails = append(allEmails, locCtx.ContactEmail)
		}
		
		// Step 2: Generate hierarchy-based emails
		hierarchy := s.osmService.Client().GetLocationHierarchy(locCtx)
		hierarchyEmails := osm.GenerateHierarchyEmails(hierarchy)
		allEmails = append(allEmails, hierarchyEmails...)
		log.Printf("Report %d: Generated %d hierarchy emails from %d levels",
			report.Seq, len(hierarchyEmails), len(hierarchy))
		
		// Step 3: Scrape website for mailto links
		if locCtx.Domain != "" {
			websiteURL := "https://" + locCtx.Domain
			scrapedEmails, err := s.osmService.Client().ScrapeEmailsFromWebsite(websiteURL)
			if err != nil {
				log.Printf("Report %d: Website scrape failed: %v", report.Seq, err)
			} else if len(scrapedEmails) > 0 {
				allEmails = append(allEmails, scrapedEmails...)
				log.Printf("Report %d: Scraped %d emails from %s", report.Seq, len(scrapedEmails), websiteURL)
			}
		}
	}
	
	// Step 4: Query Overpass for nearby POIs (may find additional buildings/orgs)
	pois, err := s.osmService.Client().QueryNearbyPOIs(report.Latitude, report.Longitude, 200)
	if err != nil {
		log.Printf("Report %d: Overpass query failed: %v", report.Seq, err)
	} else if len(pois) > 0 {
		log.Printf("Report %d: Overpass found %d nearby POIs", report.Seq, len(pois))
		
		for _, poi := range pois {
			// Direct contact email from POI
			if poi.ContactEmail != "" {
				allEmails = append(allEmails, poi.ContactEmail)
			}
			
			// Try scraping POI website
			if poi.Website != "" && len(allEmails) < 10 { // Limit scraping
				scrapedEmails, err := s.osmService.Client().ScrapeEmailsFromWebsite(poi.Website)
				if err == nil && len(scrapedEmails) > 0 {
					allEmails = append(allEmails, scrapedEmails...)
					log.Printf("Report %d: Scraped %d emails from POI %q website",
						report.Seq, len(scrapedEmails), poi.Name)
				}
			}
		}
	}
	
	// Step 5: Validate and deduplicate all collected emails
	validEmails := osm.ValidateAndFilterEmails(allEmails)
	log.Printf("Report %d: %d valid emails after filtering (from %d total)",
		report.Seq, len(validEmails), len(allEmails))
	
	// Step 6: If we found enough emails, save them
	if len(validEmails) >= 2 {
		// Limit to top 5
		if len(validEmails) > 5 {
			validEmails = validEmails[:5]
		}
		enrichedEmails := strings.Join(validEmails, ", ")
		if err := s.db.UpdateInferredContactEmails(report.Seq, enrichedEmails); err != nil {
			log.Printf("Report %d: Failed to update with enriched emails: %v", report.Seq, err)
		} else {
			log.Printf("Report %d: Enriched with %d OSM-based emails: %s",
				report.Seq, len(validEmails), enrichedEmails)
			s.osmService.SaveInferredEmails(report.Latitude, report.Longitude, enrichedEmails)
		}
		return
	}
	
	// Step 7: Fall back to LLM re-analysis with location context if we didn't find enough
	if locCtx != nil && locCtx.HasUsefulData() {
		if geminiClient, ok := s.llmClient.(*gemini.Client); ok {
			imageData, err := s.db.GetReportImage(report.Seq)
			if err != nil {
				log.Printf("Report %d: Failed to fetch image for LLM enrichment: %v", report.Seq, err)
				return
			}

			geminiLocCtx := &gemini.LocationContext{
				PrimaryName:  locCtx.PrimaryName,
				ParentOrg:    locCtx.ParentOrg,
				Operator:     locCtx.Operator,
				Domain:       locCtx.Domain,
				ContactEmail: locCtx.ContactEmail,
				LocationType: locCtx.LocationType,
				City:         locCtx.Address.City,
				State:        locCtx.Address.State,
				Country:      locCtx.Address.Country,
			}

			response, err := geminiClient.AnalyzeImageWithLocation(imageData, report.Description, geminiLocCtx)
			if err != nil {
				log.Printf("Report %d: Failed to re-analyze with location context: %v", report.Seq, err)
				return
			}

			enrichedAnalysis, err := parser.ParseAnalysis(response)
			if err != nil {
				log.Printf("Report %d: Failed to parse enriched analysis: %v", report.Seq, err)
				return
			}

			// Validate LLM-generated emails
			llmEmails := osm.ValidateAndFilterEmails(enrichedAnalysis.InferredContactEmails)
			if len(llmEmails) > 0 {
				enrichedEmails := strings.Join(llmEmails, ", ")
				if err := s.db.UpdateInferredContactEmails(report.Seq, enrichedEmails); err != nil {
					log.Printf("Report %d: Failed to update with LLM emails: %v", report.Seq, err)
				} else {
					log.Printf("Report %d: Enriched with %d LLM-inferred emails",
						report.Seq, len(llmEmails))
					s.osmService.SaveInferredEmails(report.Latitude, report.Longitude, enrichedEmails)
				}
			}
		}
	}
}

// enrichDigitalReportEmails enriches digital reports with contacts from brand_contacts table
// This runs in a goroutine to avoid blocking the main analysis flow
func (s *Service) enrichDigitalReportEmails(report *database.Report, analysis *database.ReportAnalysis) {
	brandName := strings.ToLower(strings.TrimSpace(analysis.BrandName))
	if brandName == "" {
		log.Printf("Report %d: No brand name for digital report, skipping contact enrichment", report.Seq)
		return
	}

	log.Printf("Report %d: Enriching digital report for brand %q", report.Seq, brandName)

	// Process user-provided contacts from report description
	if err := s.contactService.ProcessReportDescription(brandName, report.Description); err != nil {
		log.Printf("Report %d: Failed to process description contacts: %v", report.Seq, err)
	}

	// Get existing emails from analysis
	existingEmails := strings.Split(analysis.InferredContactEmails, ",")
	for i := range existingEmails {
		existingEmails[i] = strings.TrimSpace(existingEmails[i])
	}

	// Look up contacts for this brand
	brandContacts, err := s.contactService.GetContactsForBrand(brandName)
	if err != nil {
		log.Printf("Report %d: Failed to get contacts for brand %q: %v", report.Seq, brandName, err)
		return
	}

	// If no contacts found, try Phase 2 discovery
	if len(brandContacts) == 0 {
		log.Printf("Report %d: No contacts in DB for brand %q, attempting discovery...", report.Seq, brandName)
		
		// Infer domain from brand name (simple heuristic)
		domain := brandName + ".com"
		
		// Run discovery (LinkedIn, Twitter, GitHub)
		if err := s.contactService.DiscoverAndSaveContactsForBrand(brandName, domain); err != nil {
			log.Printf("Report %d: Discovery failed for brand %q: %v", report.Seq, brandName, err)
		}
		
		// Re-fetch contacts after discovery
		brandContacts, err = s.contactService.GetContactsForBrand(brandName)
		if err != nil || len(brandContacts) == 0 {
			log.Printf("Report %d: Still no contacts for brand %q after discovery", report.Seq, brandName)
			return
		}
	}


	log.Printf("Report %d: Found %d contacts for brand %q", report.Seq, len(brandContacts), brandName)

	// Collect all emails and social handles
	var allEmails []string
	var allSocials []string
	seen := make(map[string]bool)

	// Add existing emails first
	for _, email := range existingEmails {
		if email != "" && !seen[strings.ToLower(email)] {
			allEmails = append(allEmails, email)
			seen[strings.ToLower(email)] = true
		}
	}

	// Add emails from brand contacts (already ordered by seniority)
	for _, c := range brandContacts {
		if c.Email != "" && !seen[strings.ToLower(c.Email)] {
			allEmails = append(allEmails, c.Email)
			seen[strings.ToLower(c.Email)] = true
		}
		if c.TwitterHandle != "" {
			allSocials = append(allSocials, c.TwitterHandle)
		}
	}

	// Validate emails
	validEmails := osm.ValidateAndFilterEmails(allEmails)

	// Limit to top 5 emails
	if len(validEmails) > 5 {
		validEmails = validEmails[:5]
	}

	if len(validEmails) > 0 {
		enrichedEmails := strings.Join(validEmails, ", ")
		if err := s.db.UpdateInferredContactEmails(report.Seq, enrichedEmails); err != nil {
			log.Printf("Report %d: Failed to update with brand contact emails: %v", report.Seq, err)
		} else {
			log.Printf("Report %d: Enriched with %d brand contact emails: %s",
				report.Seq, len(validEmails), enrichedEmails)
			// Update the analysis struct so RabbitMQ publish has the data
			analysis.InferredContactEmails = enrichedEmails
		}
	}

	// Log social handles for reference (could be stored separately in future)
	if len(allSocials) > 0 {
		log.Printf("Report %d: Brand %q social handles: %v", report.Seq, brandName, allSocials)
	}
}
