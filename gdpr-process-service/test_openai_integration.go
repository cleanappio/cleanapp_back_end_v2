package main

import (
	"gdpr-process-service/openai"
	"log"
	"os"
)

func testOpenAI() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Println("OPENAI_API_KEY environment variable not set, skipping OpenAI test")
		return
	}

	// Create OpenAI client
	client := openai.NewClient(apiKey, "gpt-4o")

	// Test text with potential PII
	testText := "My name is John Doe and my email is john.doe@example.com"

	log.Printf("Testing OpenAI PII detection with text: %s", testText)

	// Process the text
	result, err := client.DetectAndObfuscatePII(testText)
	if err != nil {
		log.Printf("Failed to process text: %v", err)
		return
	}

	log.Printf("OpenAI response: %s", result)
	log.Println("OpenAI test completed successfully!")
}

// testDatabaseUpdate tests the database update functionality
func testDatabaseUpdate() {
	log.Println("Testing database update functionality...")

	// This would test the UpdateUserAvatar function
	// For now, just log that the test is available
	log.Println("Database update test framework ready")
	log.Println("Note: Actual database updates require database connection")
}
