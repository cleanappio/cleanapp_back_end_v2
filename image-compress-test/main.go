package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"image-compress-test/image"
)

func main() {
	// Check if filename argument is provided
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <image-file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s photo.jpg\n", os.Args[0])
		os.Exit(1)
	}

	inputFile := os.Args[1]

	// Check if input file exists
	if _, err := os.Stat(inputFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: File '%s' does not exist\n", inputFile)
		os.Exit(1)
	}

	// Read the input image file
	imageData, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file '%s': %v\n", inputFile, err)
		os.Exit(1)
	}

	fmt.Printf("Original image size: %d bytes\n", len(imageData))

	// Compress the image
	compressedData, err := image.CompressImage(imageData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error compressing image: %v\n", err)
		os.Exit(1)
	}

	// Generate output filename
	ext := filepath.Ext(inputFile)
	baseName := strings.TrimSuffix(inputFile, ext)
	outputFile := baseName + "-compressed.jpg"

	// Write the compressed image to output file
	err = os.WriteFile(outputFile, compressedData, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing compressed image to '%s': %v\n", outputFile, err)
		os.Exit(1)
	}

	fmt.Printf("Compressed image size: %d bytes\n", len(compressedData))
	fmt.Printf("Compression ratio: %.2f%%\n", float64(len(compressedData))/float64(len(imageData))*100)
	fmt.Printf("Compressed image saved as: %s\n", outputFile)
}
