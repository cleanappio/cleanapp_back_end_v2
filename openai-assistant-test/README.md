# OpenAI Assistant Image Upload Test

This Go program demonstrates how to upload an image to an OpenAI assistant using the OpenAI Assistant API, and retrieve the assistant's response.

## Features

- Uploads image files to OpenAI (supports JPEG, PNG, GIF, WebP)
- Creates a thread for conversation
- Adds the image as a message to the thread
- Runs the assistant to process the image
- Retrieves and displays the assistant's response

## Prerequisites

- Go 1.21 or later
- OpenAI API key with access to the Assistant API
- An existing OpenAI assistant ID

## Environment Variables

Before running the program, you need to set the following environment variables:

```bash
export OPENAI_API_KEY="<openai_api_key>"
export OPENAI_ASSISTANT_ID="<openai_assistant_id>"
```

## Usage

```bash
go run main.go <image_path>
```

### Example

```bash
# Set environment variables
export OPENAI_API_KEY="<openai_api_key>"
export OPENAI_ASSISTANT_ID="<openai_assistant_id>"

# Run the program
go run main.go ./image.jpg
```

## Parameters

- `image_path`: Path to the image file you want to upload

## Environment Variables

- `OPENAI_API_KEY`: Your OpenAI API key (starts with "sk-")
- `OPENAI_ASSISTANT_ID`: Your OpenAI assistant ID (starts with "asst_")

## How it works

1. **File Upload**: Uploads the image file to OpenAI's servers
2. **Thread Creation**: Creates a new conversation thread
3. **Message Addition**: Adds the image as a user message to the thread
4. **Run Creation**: Starts a run with your assistant
5. **Wait for Completion**: Polls until the assistant finishes processing
6. **Response Retrieval**: Gets and displays the assistant's response

## Supported Image Formats

- JPEG (.jpg, .jpeg)
- PNG (.png)
- GIF (.gif)
- WebP (.webp)

## Error Handling

The program includes comprehensive error handling for:
- File not found
- API authentication errors
- Network issues
- Invalid assistant ID
- Run failures

## Using the Shell Script

You can also use the provided shell script for easier execution:

```bash
# Set environment variables
export OPENAI_API_KEY="<openai_api_key>"
export OPENAI_ASSISTANT_ID="<openai_assistant_id>"

# Run with the script
./run.sh ./image.jpg
```

The script will:
- Validate that environment variables are set
- Check if the image file exists
- Validate the format of API key and assistant ID
- Run the Go program with proper error handling

## Output

The program will display:
- Progress updates for each step
- File ID, Thread ID, and Run ID for debugging
- The assistant's final response
- Success/failure status

## Notes

- The assistant must be configured to handle vision/image inputs
- The program waits for the run to complete before retrieving the response
- All API calls use proper error handling and logging
- Environment variables provide better security than command line arguments 
