package llm

// Client abstracts an LLM provider used by the analyzer.
// Implementations must be concurrency-safe if used across goroutines.
type Client interface {
	// AnalyzeImage takes raw image bytes and a description/context string,
	// and returns a single JSON string per the analyzer schema.
	AnalyzeImage(imageData []byte, description string) (string, error)
	// TranslateAnalysis translates JSON values to a target human language name (e.g., "German").
	TranslateAnalysis(jsonText, targetLanguage string) (string, error)
	// SourceName returns a short provider label to persist in the database (e.g., "ChatGPT", "Gemini").
	SourceName() string
}
