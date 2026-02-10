package face_detector

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/apex/log"
)

// Client handles communication with the face detector service
type Client struct {
	baseURL               string
	faceDetectorPortStart int
	httpClient            *http.Client
}

// ProcessImageRequest represents the request to the face detector service
type ProcessImageRequest struct {
	Image string `json:"image"`
}

// ProcessImageResponse represents the response from the face detector service
type ProcessImageResponse struct {
	Message        string                 `json:"message"`
	EstimatedSize  int                    `json:"estimated_size"`
	FacesDetected  int                    `json:"faces_detected"`
	ProcessedImage string                 `json:"processed_image"`
	ImageInfo      map[string]interface{} `json:"image_info"`
	Status         string                 `json:"status"`
}

// NewClient creates a new face detector client
func NewClient(baseURL string, faceDetectorPortStart int) *Client {
	return &Client{
		baseURL:               baseURL,
		faceDetectorPortStart: faceDetectorPortStart,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // 60 second timeout for image processing
		},
	}
}

// ProcessImage sends an image to the face detector service for processing
func (c *Client) ProcessImage(imageData []byte, processNumber int) ([]byte, bool, error) {
	// Encode image data to base64
	base64Image := base64.StdEncoding.EncodeToString(imageData)

	// Create request payload
	request := ProcessImageRequest{
		Image: base64Image,
	}

	// Marshal request to JSON
	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s:%d/process-base64", c.baseURL, c.faceDetectorPortStart+processNumber)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, false, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	log.Infof("Sending image to face detector service: %s, image size: %d bytes", url, len(imageData))

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to send request to face detector service: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("face detector service returned status %d", resp.StatusCode)
	}

	// Parse response
	var response ProcessImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, false, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check if processing was successful
	if response.Status != "completed" {
		return nil, false, fmt.Errorf("face detector service returned status: %s", response.Status)
	}

	// Decode processed image from base64
	processedImageData, err := base64.StdEncoding.DecodeString(response.ProcessedImage)
	if err != nil {
		return nil, false, fmt.Errorf("failed to decode processed image: %w", err)
	}

	// Determine if faces were detected
	facesDetected := response.FacesDetected > 0

	log.Infof("Successfully processed image: faces detected: %d, processed size: %d bytes, has faces: %t",
		response.FacesDetected, len(processedImageData), facesDetected)

	return processedImageData, facesDetected, nil
}
