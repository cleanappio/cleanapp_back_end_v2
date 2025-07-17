#!/bin/bash

# Check if .env file exists
if [ ! -f .env ]; then
    echo "Error: .env file not found!"
    echo "Please create a .env file with your environment variables."
    echo "Example .env file:"
    echo "OPENAI_API_KEY=your_openai_api_key"
    echo "GEMINI_API_KEY=your_gemini_api_key"
    exit 1
fi

# Read .env file and export variables
echo "Loading environment variables from .env file..."
while IFS= read -r line || [ -n "$line" ]; do
    # Skip empty lines and comments
    if [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]]; then
        continue
    fi
    
    # Export the variable
    export "$line"
    echo "Exported: $line"
done < .env

echo "Environment variables loaded successfully!"

# Check if image path is provided
if [ $# -eq 0 ]; then
    echo "Error: No image path provided!"
    echo "Usage: ./run.sh <image_path>"
    echo "Example: ./run.sh ./test_image.jpg"
    exit 1
fi

# Check if the image file exists
if [ ! -f "$1" ]; then
    echo "Error: Image file '$1' not found!"
    exit 1
fi

# Check if OPENAI_API_KEY is set
if [ -z "$OPENAI_API_KEY" ]; then
    echo "Warning: OPENAI_API_KEY is not set in .env file"
    echo "The program will exit when trying to use OpenAI API"
fi

# Check if GEMINI_API_KEY is set
if [ -z "$GEMINI_API_KEY" ]; then
    echo "Info: GEMINI_API_KEY is not set in .env file"
    echo "Gemini API features will be skipped"
fi

echo "Running main.go with image: $1"
echo "----------------------------------------"

# Run the Go program
go run main.go "$1"
