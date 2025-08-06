#!/bin/bash

# OpenAI Assistant Image Upload Test Runner
# This script helps you run the Go program with environment variables

# Check if the correct number of arguments is provided
if [ $# -ne 1 ]; then
    echo "Usage: ./run.sh <image_path>"
    echo ""
    echo "Example:"
    echo "  ./run.sh ./image.jpg"
    echo ""
    echo "Environment variables required:"
    echo "  OPENAI_API_KEY: Your OpenAI API key (starts with 'sk-')"
    echo "  OPENAI_ASSISTANT_ID: Your OpenAI assistant ID (starts with 'asst_')"
    echo ""
    echo "Set them with:"
    echo "  export OPENAI_API_KEY=sk-your-api-key-here"
    echo "  export OPENAI_ASSISTANT_ID=asst-your-assistant-id-here"
    exit 1
fi

IMAGE_PATH=$1

# Check if the image file exists
if [ ! -f "$IMAGE_PATH" ]; then
    echo "Error: Image file '$IMAGE_PATH' does not exist"
    exit 1
fi

# Check if environment variables are set
if [ -z "$OPENAI_API_KEY" ]; then
    echo "Error: OPENAI_API_KEY environment variable is not set"
    echo "Please set it with: export OPENAI_API_KEY=sk-your-api-key-here"
    exit 1
fi

if [ -z "$OPENAI_ASSISTANT_ID" ]; then
    echo "Error: OPENAI_ASSISTANT_ID environment variable is not set"
    echo "Please set it with: export OPENAI_ASSISTANT_ID=asst-your-assistant-id-here"
    exit 1
fi

# Check if assistant ID format is correct
if [[ ! "$OPENAI_ASSISTANT_ID" =~ ^asst_ ]]; then
    echo "Warning: Assistant ID should start with 'asst_'"
fi

# Check if API key format is correct
if [[ ! "$OPENAI_API_KEY" =~ ^sk- ]]; then
    echo "Warning: API key should start with 'sk-'"
fi

echo "Running OpenAI Assistant Image Upload Test..."
echo "Image: $IMAGE_PATH"
echo "Assistant ID: $OPENAI_ASSISTANT_ID"
echo ""

# Run the Go program
go run main.go "$IMAGE_PATH" 