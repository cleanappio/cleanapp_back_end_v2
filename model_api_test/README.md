# GPT API Test with Gemini Integration

This program demonstrates image analysis using both OpenAI GPT-4 Vision API and Google Gemini API.

## Features

- **OpenAI GPT-4 Vision API**: Image analysis and text translation
- **Google Gemini API**: Alternative image analysis and text translation using the official Google AI Go client library
- **Automatic URL Detection**: Chooses appropriate analysis prompts based on image content
- **Multi-language Support**: Translates analysis results to multiple languages
- **Multiple Image Formats**: Supports JPEG, PNG, GIF, and WebP
- **Type Safety**: Uses official Google AI Go client library for better type safety and maintainability

## Setup

### Prerequisites

1. **OpenAI API Key** (Required)
   - Get your API key from [OpenAI Platform](https://platform.openai.com/api-keys)
   - Set environment variable: `export OPENAI_API_KEY=your_openai_api_key`

2. **Google Gemini API Key** (Optional)
   - Get your API key from [Google AI Studio](https://makersuite.google.com/app/apikey)
   - Set environment variable: `export GEMINI_API_KEY=your_gemini_api_key`

### Installation

```bash
# Navigate to the project directory
cd gpt_api_test

# Install dependencies
go mod tidy

# Build the program
go build -o main .

# Or run directly
go run main.go <image_path>
```

### Dependencies

The program uses the following key dependencies:
- `github.com/google/generative-ai-go/genai`: Official Google AI Go client library for Gemini API
- `google.golang.org/api/option`: Google API options for authentication

### Run Script

The `run.sh` script provides a convenient way to run the program:

- **Automatic .env loading**: Reads and exports all environment variables from `.env` file
- **Input validation**: Checks if the image file exists before running
- **API key validation**: Warns about missing API keys
- **Error handling**: Provides helpful error messages and usage instructions

To use the run script:
1. Copy `env.example` to `.env` and add your API keys
2. Make the script executable: `chmod +x run.sh`
3. Run: `./run.sh ./your_image.jpg`

## Usage

### Basic Usage

#### Option 1: Using the run script (Recommended)

```bash
# Copy the environment template
cp env.example .env

# Edit .env file with your API keys
nano .env  # or use your preferred editor

# Make the script executable (if needed)
chmod +x run.sh

# Run the program
./run.sh ./path/to/your/image.jpg
```

#### Option 2: Manual execution

```bash
# Analyze an image
go run main.go ./path/to/your/image.jpg

# Example with environment variables
export OPENAI_API_KEY=your_openai_key
export GEMINI_API_KEY=your_gemini_key
go run main.go ./test_image.png
```

### What the Program Does

1. **Image Classification**: Determines if the image contains a website/URL
2. **Analysis**: Uses appropriate prompt based on classification:
   - **Digital Error Prompt**: For images containing websites/URLs
   - **Litter/Hazard Prompt**: For general images
3. **Translation**: Translates analysis to Montenegrin
4. **Gemini Demo**: If `GEMINI_API_KEY` is set, demonstrates Gemini API usage

## API Functions

### OpenAI Functions

- `callOpenAI(apiKey, base64Image, prompt)`: Analyzes images with GPT-4 Vision
- `callOpenAITranslation(apiKey, text, targetLanguage)`: Translates text

### Gemini Functions

- `CallGemini(apiKey, base64Image, prompt)`: Analyzes images with Gemini using the official Google AI Go client library
- `CallGeminiTranslation(apiKey, text, targetLanguage)`: Translates text with Gemini using the official Google AI Go client library

## Example Output

```
Step 1: Classifying image...
Classification response: I can see a website on the screen...

No URL detected. Using litter/hazard prompt...
Step 2: Analyzing with appropriate prompt...
Analysis response: {"title": "Plastic bottle found", "description": "A plastic water bottle..."}

Step 4: Translating to Montenegro language...
Translation response: {"title": "Pronađena plastična flaša", "description": "Plastična flaša vode..."}

=== Gemini API Example ===
Step 5: Analyzing with Gemini...
Gemini analysis response: {"title": "Plastic bottle detected", "description": "A plastic water bottle..."}
Step 6: Translating with Gemini...
Gemini translation response: {"title": "Pronađena plastična flaša", "description": "Plastična flaša vode..."}
```

## Supported Image Formats

- JPEG (.jpg, .jpeg)
- PNG (.png)
- GIF (.gif)
- WebP (.webp)

## Error Handling

The program includes comprehensive error handling for:
- Missing API keys
- Invalid image files
- API request failures
- Response parsing errors
- Network connectivity issues

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `OPENAI_API_KEY` | Yes | OpenAI API key for GPT-4 Vision |
| `GEMINI_API_KEY` | No | Google AI API key for Gemini |

## Troubleshooting

### Common Issues

1. **"OPENAI_API_KEY is not set"**
   - Set your OpenAI API key: `export OPENAI_API_KEY=your_key`

2. **"GEMINI_API_KEY is not set (optional)"**
   - This is just informational. Set the key to enable Gemini features

3. **"failed to open image file"**
   - Check that the image path is correct and the file exists

4. **"API error (status 401)"**
   - Check that your API key is valid and has sufficient credits

5. **"API error (status 429)"**
   - You've hit the rate limit. Wait a moment and try again

## Development

### Adding New Prompts

To add new analysis prompts, define them as constants:

```go
const myCustomPrompt = `
Your custom prompt here...
`
```

### Extending Language Support

The translation functions support any language that the APIs support. Simply pass the language name:

```go
// Examples
CallGeminiTranslation(apiKey, text, "Spanish")
CallGeminiTranslation(apiKey, text, "French")
CallGeminiTranslation(apiKey, text, "German")
```

### API Response Format

Both APIs return JSON responses that are parsed and displayed. The program handles different response formats automatically.

### Benefits of Using Official Google AI Go Client Library

The Gemini implementation uses the official `github.com/google/generative-ai-go/genai` library, which provides:

- **Type Safety**: Compile-time type checking for API requests and responses
- **Automatic Authentication**: Built-in support for API key authentication
- **Error Handling**: Comprehensive error types and handling
- **Future Compatibility**: Automatic updates for new API features
- **Better Performance**: Optimized HTTP client and connection pooling
- **Documentation**: Well-documented API with examples 