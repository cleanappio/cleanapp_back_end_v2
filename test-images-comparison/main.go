package main

import (
	"cleanapp/test-images-comparison/openai"
	"fmt"
	"log"
	"math"
	"os"
)

func main() {
	// Check if API key is provided
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Check command line arguments
	if len(os.Args) < 3 {
		log.Fatal("Usage: go run main.go <image1_path> <image2_path> [original_description] [lat1] [lng1] [lat2] [lng2]")
	}

	image1Path := os.Args[1]
	image2Path := os.Args[2]
	originalDescription := os.Args[3]
	// Default coordinates (can be overridden by command line args)
	lat1, lng1 := 47.3205, 8.52144
	lat2, lng2 := 47.3203, 8.5214

	radiusMeters := 20.0

	latRadiusDegrees := radiusMeters / 111320.0
	lonRadiusDegrees := radiusMeters / (111320.0 * math.Cos(lat2*math.Pi/180.0))

	log.Printf("latRadiusDegrees: %f, lonRadiusDegrees: %f", latRadiusDegrees, lonRadiusDegrees)

	minLat := lat2 - latRadiusDegrees
	maxLat := lat2 + latRadiusDegrees
	minLng := lng2 - lonRadiusDegrees
	maxLng := lng2 + lonRadiusDegrees

	log.Printf("minLat: %f, maxLat: %f, minLng: %f, maxLng: %f", minLat, maxLat, minLng, maxLng)

	// Parse coordinates if provided
	if len(os.Args) >= 8 {
		if _, err := fmt.Sscanf(os.Args[4], "%f", &lat1); err != nil {
			log.Printf("Warning: Invalid lat1, using default: %v", err)
		}
		if _, err := fmt.Sscanf(os.Args[5], "%f", &lng1); err != nil {
			log.Printf("Warning: Invalid lng1, using default: %v", err)
		}
		if _, err := fmt.Sscanf(os.Args[6], "%f", &lat2); err != nil {
			log.Printf("Warning: Invalid lat2, using default: %v", err)
		}
		if _, err := fmt.Sscanf(os.Args[7], "%f", &lng2); err != nil {
			log.Printf("Warning: Invalid lng2, using default: %v", err)
		}
	}

	// Read first image
	image1, err := os.ReadFile(image1Path)
	if err != nil {
		log.Fatalf("Failed to read image1 from %s: %v", image1Path, err)
	}

	// Read second image
	image2, err := os.ReadFile(image2Path)
	if err != nil {
		log.Fatalf("Failed to read image2 from %s: %v", image2Path, err)
	}

	// Create OpenAI client
	c := openai.NewClient(apiKey, "gpt-4o-mini")

	// Compare images
	fmt.Printf("Comparing images:\n")
	fmt.Printf("  Image 1: %s (lat: %f, lng: %f)\n", image1Path, lat1, lng1)
	fmt.Printf("  Image 2: %s (lat: %f, lng: %f)\n", image2Path, lat2, lng2)
	fmt.Printf("\nCalling OpenAI API...\n")

	samePlaceProbability, litterOrHazardRemoved, err := c.CompareImages(image1, image2, lat1, lng1, lat2, lng2, originalDescription)
	if err != nil {
		log.Fatalf("Failed to compare images: %v", err)
	}

	// Print results
	fmt.Printf("\n=== Comparison Results ===\n")
	fmt.Printf("Same Place Probability: %.2f%%\n", samePlaceProbability*100)
	fmt.Printf("Litter or Hazard Removed: %t\n", litterOrHazardRemoved)
}
