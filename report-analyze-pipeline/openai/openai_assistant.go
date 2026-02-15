package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
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

// Client represents an OpenAI API client
type AssistantClient struct {
	apiKey      string
	assistantID string
	client      *http.Client
}

// NewClient creates a new OpenAI client
func NewAssistantClient(apiKey, assistantID string) *AssistantClient {
	return &AssistantClient{
		apiKey:      apiKey,
		assistantID: assistantID,
		client:      &http.Client{Timeout: 60 * time.Second},
	}
}

// uploadFile uploads a file to OpenAI and returns the file ID
func (c *AssistantClient) uploadFile(fileBuf []byte) (string, error) {
	// Create multipart form data
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add the file
	part, err := writer.CreateFormFile("file", "image.jpg")
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}

	_, err = part.Write(fileBuf)
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

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("OpenAI-Beta", "assistants=v2")

	// Send request
	resp, err := c.client.Do(req)
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
func (c *AssistantClient) createThread() (string, error) {
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

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", "assistants=v2")

	resp, err := c.client.Do(req)
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

// addMessageToThread adds a message with an image and an extra description to the thread
func (c *AssistantClient) addMessageToThread(threadID, fileID, description string) error {
	var reqBody map[string]any
	if description == "" {
		reqBody = map[string]any{
			"role": "user",
			"content": []map[string]any{
				{
					"type": "image_file",
					"image_file": map[string]any{
						"file_id": fileID,
					},
				},
			},
		}
	} else {
		reqBody = map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type": "image_file",
					"image_file": map[string]any{
						"file_id": fileID,
					},
				},
				map[string]any{
					"type": "text",
					"text": description,
				},
			},
		}
	}

	log.Printf("reqBody: %+v", reqBody)

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", openAIBaseURL+"/threads/"+threadID+"/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", "assistants=v2")

	resp, err := c.client.Do(req)
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
func (c *AssistantClient) createRun(threadID string) (string, error) {
	reqBody := map[string]interface{}{
		"assistant_id": c.assistantID,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", openAIBaseURL+"/threads/"+threadID+"/runs", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", "assistants=v2")

	resp, err := c.client.Do(req)
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
func (c *AssistantClient) waitForRunCompletion(threadID, runID string) error {
	for {
		req, err := http.NewRequest("GET", openAIBaseURL+"/threads/"+threadID+"/runs/"+runID, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("OpenAI-Beta", "assistants=v2")

		resp, err := c.client.Do(req)
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
func (c *AssistantClient) getMessages(threadID string) ([]ThreadMessage, error) {
	req, err := http.NewRequest("GET", openAIBaseURL+"/threads/"+threadID+"/messages", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("OpenAI-Beta", "assistants=v2")

	resp, err := c.client.Do(req)
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

// AnalyseImageWithAssistant takes an image buffer and returns an analysis result
func (c *AssistantClient) AnalyseImageWithAssistant(imageBuffer []byte, description string) (string, error) {
	// Step 1: Upload the image file
	fileID, err := c.uploadFile(imageBuffer)
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	// Step 2: Create a thread
	threadID, err := c.createThread()
	if err != nil {
		return "", fmt.Errorf("failed to create thread: %w", err)
	}

	// Step 3: Add the image message to the thread
	err = c.addMessageToThread(threadID, fileID, description)
	if err != nil {
		return "", fmt.Errorf("failed to add message to thread: %w", err)
	}

	// Step 4: Create a run with the assistant
	runID, err := c.createRun(threadID)
	if err != nil {
		return "", fmt.Errorf("failed to create run: %w", err)
	}

	// Step 5: Wait for the run to complete
	err = c.waitForRunCompletion(threadID, runID)
	if err != nil {
		return "", fmt.Errorf("failed to wait for run completion: %w", err)
	}

	// Step 6: Get the messages from the thread
	messages, err := c.getMessages(threadID)
	if err != nil {
		return "", fmt.Errorf("failed to get messages: %w", err)
	}

	// Extract the assistant's response
	var response string
	for _, message := range messages {
		if message.Role == "assistant" {
			for _, content := range message.Content {
				if content.Type == "text" && content.Text != nil {
					response = content.Text.Value
					break
				}
			}
			if response != "" {
				break
			}
		}
	}

	if response == "" {
		return "", fmt.Errorf("no response received from assistant")
	}

	return response, nil
}
