package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	openAIBaseURL = "https://api.openai.com/v1"
)

// AssistantAPIResponse represents the response from the assistant API
type AssistantAPIResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	CreatedAt int64  `json:"created_at"`
	ThreadID  string `json:"thread_id"`
	Status    string `json:"status"`
	StartedAt *int64 `json:"started_at,omitempty"`
	EndedAt   *int64 `json:"ended_at,omitempty"`
	LastError *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"last_error,omitempty"`
	ExpiresAt    *int64 `json:"expires_at,omitempty"`
	Model        string `json:"model"`
	Instructions string `json:"instructions,omitempty"`
	Tools        []struct {
		Type string `json:"type"`
	} `json:"tools"`
	FileIDs  []string               `json:"file_ids"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ThreadMessage represents a message in a thread
type ThreadMessage struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	CreatedAt int64  `json:"created_at"`
	ThreadID  string `json:"thread_id"`
	Role      string `json:"role"`
	Content   []struct {
		Type string `json:"type"`
		Text *struct {
			Value       string        `json:"value"`
			Annotations []interface{} `json:"annotations"`
		} `json:"text,omitempty"`
		ImageFile *struct {
			FileID string `json:"file_id"`
		} `json:"image_file,omitempty"`
	} `json:"content"`
	AssistantID *string                `json:"assistant_id,omitempty"`
	RunID       *string                `json:"run_id,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// FileUploadResponse represents the response from file upload
type FileUploadResponse struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Bytes     int    `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
}

// ThreadResponse represents the response from creating a thread
type ThreadResponse struct {
	ID        string                 `json:"id"`
	Object    string                 `json:"object"`
	CreatedAt int64                  `json:"created_at"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// RunResponse represents the response from creating a run
type RunResponse struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	CreatedAt   int64  `json:"created_at"`
	ThreadID    string `json:"thread_id"`
	AssistantID string `json:"assistant_id"`
	Status      string `json:"status"`
	StartedAt   *int64 `json:"started_at,omitempty"`
	ExpiresAt   *int64 `json:"expires_at,omitempty"`
	CompletedAt *int64 `json:"completed_at,omitempty"`
	CancelledAt *int64 `json:"cancelled_at,omitempty"`
	FailedAt    *int64 `json:"failed_at,omitempty"`
	LastError   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"last_error,omitempty"`
	Model        string                 `json:"model"`
	Instructions string                 `json:"instructions"`
	Tools        []interface{}          `json:"tools"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// uploadFile uploads a file to OpenAI and returns the file ID
func uploadFile(apiKey, filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create multipart form data
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add the file
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}

	_, err = io.Copy(part, file)
	if err != nil {
		return "", fmt.Errorf("failed to copy file data: %w", err)
	}

	// Add the purpose
	err = writer.WriteField("purpose", "vision")
	if err != nil {
		return "", fmt.Errorf("failed to write purpose field: %w", err)
	}

	writer.Close()

	// Create request
	req, err := http.NewRequest("POST", openAIBaseURL+"/files", &buf)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("OpenAI-Beta", "assistants=v2")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var fileResp FileUploadResponse
	if err := json.Unmarshal(body, &fileResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return fileResp.ID, nil
}

// createThread creates a new thread
func createThread(apiKey string) (string, error) {
	reqBody := map[string]interface{}{
		"messages": []interface{}{},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", openAIBaseURL+"/threads", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", "assistants=v2")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var threadResp ThreadResponse
	if err := json.Unmarshal(body, &threadResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return threadResp.ID, nil
}

// addMessageToThread adds a message with an image to the thread
func addMessageToThread(apiKey, threadID, fileID string) error {
	reqBody := map[string]interface{}{
		"role": "user",
		"content": []map[string]interface{}{
			{
				"type": "image_file",
				"image_file": map[string]interface{}{
					"file_id": fileID,
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", openAIBaseURL+"/threads/"+threadID+"/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", "assistants=v2")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// createRun creates a run with the assistant
func createRun(apiKey, threadID, assistantID string) (string, error) {
	reqBody := map[string]interface{}{
		"assistant_id": assistantID,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", openAIBaseURL+"/threads/"+threadID+"/runs", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", "assistants=v2")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var runResp RunResponse
	if err := json.Unmarshal(body, &runResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return runResp.ID, nil
}

// waitForRunCompletion waits for the run to complete
func waitForRunCompletion(apiKey, threadID, runID string) error {
	for {
		req, err := http.NewRequest("GET", openAIBaseURL+"/threads/"+threadID+"/runs/"+runID, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("OpenAI-Beta", "assistants=v2")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
		}

		var runResp RunResponse
		if err := json.Unmarshal(body, &runResp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		switch runResp.Status {
		case "completed":
			return nil
		case "failed":
			if runResp.LastError != nil {
				return fmt.Errorf("run failed: %s - %s", runResp.LastError.Code, runResp.LastError.Message)
			}
			return fmt.Errorf("run failed")
		case "cancelled":
			return fmt.Errorf("run was cancelled")
		case "expired":
			return fmt.Errorf("run expired")
		default:
			// Still running, wait and try again
			time.Sleep(1 * time.Second)
		}
	}
}

// getMessages retrieves messages from the thread
func getMessages(apiKey, threadID string) ([]ThreadMessage, error) {
	req, err := http.NewRequest("GET", openAIBaseURL+"/threads/"+threadID+"/messages", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("OpenAI-Beta", "assistants=v2")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var response struct {
		Object  string          `json:"object"`
		Data    []ThreadMessage `json:"data"`
		FirstID string          `json:"first_id"`
		LastID  string          `json:"last_id"`
		HasMore bool            `json:"has_more"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return response.Data, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <image_path>")
		fmt.Println("Example: go run main.go ./image.jpg")
		fmt.Println("")
		fmt.Println("Environment variables required:")
		fmt.Println("  OPENAI_API_KEY: Your OpenAI API key")
		fmt.Println("  OPENAI_ASSISTANT_ID: Your OpenAI assistant ID")
		return
	}

	imagePath := os.Args[1]

	// Read API key from environment variable
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("Error: OPENAI_API_KEY environment variable is not set")
		fmt.Println("Please set it with: export OPENAI_API_KEY=sk-your-api-key-here")
		return
	}

	// Read assistant ID from environment variable
	assistantID := os.Getenv("OPENAI_ASSISTANT_ID")
	if assistantID == "" {
		fmt.Println("Error: OPENAI_ASSISTANT_ID environment variable is not set")
		fmt.Println("Please set it with: export OPENAI_ASSISTANT_ID=asst-your-assistant-id-here")
		return
	}

	// Check if image file exists
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		fmt.Printf("Error: Image file '%s' does not exist\n", imagePath)
		return
	}

	fmt.Printf("Uploading image: %s\n", imagePath)
	fmt.Printf("Assistant ID: %s\n", assistantID)

	// Step 1: Upload the image file
	fmt.Println("\nStep 1: Uploading image file...")
	fileID, err := uploadFile(apiKey, imagePath)
	if err != nil {
		fmt.Printf("Error uploading file: %v\n", err)
		return
	}
	fmt.Printf("File uploaded successfully. File ID: %s\n", fileID)

	// Step 2: Create a thread
	fmt.Println("\nStep 2: Creating thread...")
	threadID, err := createThread(apiKey)
	if err != nil {
		fmt.Printf("Error creating thread: %v\n", err)
		return
	}
	fmt.Printf("Thread created successfully. Thread ID: %s\n", threadID)

	// Step 3: Add the image message to the thread
	fmt.Println("\nStep 3: Adding image message to thread...")
	err = addMessageToThread(apiKey, threadID, fileID)
	if err != nil {
		fmt.Printf("Error adding message to thread: %v\n", err)
		return
	}
	fmt.Println("Image message added successfully.")

	// Step 4: Create a run with the assistant
	fmt.Println("\nStep 4: Creating run with assistant...")
	runID, err := createRun(apiKey, threadID, assistantID)
	if err != nil {
		fmt.Printf("Error creating run: %v\n", err)
		return
	}
	fmt.Printf("Run created successfully. Run ID: %s\n", runID)

	// Step 5: Wait for the run to complete
	fmt.Println("\nStep 5: Waiting for run to complete...")
	err = waitForRunCompletion(apiKey, threadID, runID)
	if err != nil {
		fmt.Printf("Error waiting for run completion: %v\n", err)
		return
	}
	fmt.Println("Run completed successfully.")

	// Step 6: Get the messages from the thread
	fmt.Println("\nStep 6: Retrieving messages...")
	messages, err := getMessages(apiKey, threadID)
	if err != nil {
		fmt.Printf("Error getting messages: %v\n", err)
		return
	}

	// Display the assistant's response
	fmt.Println("\n=== Assistant Response ===")
	for _, message := range messages {
		if message.Role == "assistant" {
			for _, content := range message.Content {
				if content.Type == "text" && content.Text != nil {
					fmt.Println(content.Text.Value)
				}
			}
		}
	}

	fmt.Println("\n=== Process completed successfully ===")
}
