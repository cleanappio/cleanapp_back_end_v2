package image

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// createTestImage creates a test JPEG image with specified dimensions
func createTestImage(width, height int) ([]byte, error) {
	// Create a simple test image
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill with a simple pattern
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x + y) % 256),
				G: uint8((x * 2) % 256),
				B: uint8((y * 2) % 256),
				A: 255,
			})
		}
	}

	// Encode as JPEG
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95})
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func TestCompressImage(t *testing.T) {
	// Test with a large image that should be compressed
	originalData, err := createTestImage(2000, 1500)
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}

	compressedData, err := CompressImage(originalData)
	if err != nil {
		t.Fatalf("Failed to compress image: %v", err)
	}

	// Verify the compressed image is smaller
	if len(compressedData) >= len(originalData) {
		t.Errorf("Compressed image should be smaller: original=%d, compressed=%d",
			len(originalData), len(compressedData))
	}

	// Decode the compressed image to verify dimensions
	img, _, err := image.Decode(bytes.NewReader(compressedData))
	if err != nil {
		t.Fatalf("Failed to decode compressed image: %v", err)
	}

	bounds := img.Bounds()
	height := bounds.Dy()

	// Verify height is within limits
	if height > maxImageHeight {
		t.Errorf("Compressed image height %d exceeds max height %d", height, maxImageHeight)
	}

	// Verify aspect ratio is preserved (approximately)
	originalImg, _, err := image.Decode(bytes.NewReader(originalData))
	if err != nil {
		t.Fatalf("Failed to decode original image: %v", err)
	}

	originalBounds := originalImg.Bounds()
	originalWidth := originalBounds.Dx()
	originalHeight := originalBounds.Dy()
	width := bounds.Dx()

	expectedWidth := int(float64(originalWidth) * float64(height) / float64(originalHeight))
	tolerance := 2 // Allow small rounding differences

	if abs(width-expectedWidth) > tolerance {
		t.Errorf("Aspect ratio not preserved: original=%dx%d, compressed=%dx%d, expected width=%d",
			originalWidth, originalHeight, width, height, expectedWidth)
	}
}

func TestCompressImageSmall(t *testing.T) {
	// Test with a small image that shouldn't be compressed
	originalData, err := createTestImage(800, 600)
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}

	compressedData, err := CompressImage(originalData)
	if err != nil {
		t.Fatalf("Failed to compress image: %v", err)
	}

	// For small images, the data should be the same or very similar
	if len(compressedData) > len(originalData)*2 {
		t.Errorf("Small image was over-compressed: original=%d, compressed=%d",
			len(originalData), len(compressedData))
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
