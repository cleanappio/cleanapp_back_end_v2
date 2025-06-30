package image

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"

	"github.com/apex/log"
	"github.com/rwcarlsen/goexif/exif"
	"golang.org/x/image/draw"
)

const (
	maxImageDimension = 512 // Maximum width or height in pixels
	minImageQuality   = 85
)

// GetImageOrientation extracts the EXIF orientation from JPEG data using goexif library
func GetImageOrientation(data []byte) int {
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return 1 // Default orientation if no EXIF data or error
	}

	orientation, err := x.Get(exif.Orientation)
	if err != nil {
		return 1 // Default orientation if orientation tag not found
	}

	orientVal, err := orientation.Int(0)
	if err != nil {
		return 1 // Default orientation if value cannot be read
	}

	return orientVal
}

// CorrectImageOrientation applies the correct orientation to the image
func CorrectImageOrientation(img image.Image, orientation int) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	switch orientation {
	case 2: // Flip horizontal
		newImg := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				newImg.Set(width-1-x, y, img.At(x, y))
			}
		}
		return newImg
	case 3: // Rotate 180
		newImg := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				newImg.Set(width-1-x, height-1-y, img.At(x, y))
			}
		}
		return newImg
	case 4: // Flip vertical
		newImg := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				newImg.Set(x, height-1-y, img.At(x, y))
			}
		}
		return newImg
	case 5: // Transpose (rotate 90 clockwise and flip)
		newImg := image.NewRGBA(image.Rect(0, 0, height, width))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				newImg.Set(y, width-1-x, img.At(x, y))
			}
		}
		return newImg
	case 6: // Rotate 90 clockwise
		newImg := image.NewRGBA(image.Rect(0, 0, height, width))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				newImg.Set(height-1-y, x, img.At(x, y))
			}
		}
		return newImg
	case 7: // Transverse (rotate 90 counter-clockwise and flip)
		newImg := image.NewRGBA(image.Rect(0, 0, height, width))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				newImg.Set(height-1-y, x, img.At(x, y))
			}
		}
		return newImg
	case 8: // Rotate 90 counter-clockwise
		newImg := image.NewRGBA(image.Rect(0, 0, height, width))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				newImg.Set(y, width-1-x, img.At(x, y))
			}
		}
		return newImg
	default: // Orientation 1 or unknown
		return img
	}
}

// CompressImage compresses a JPEG image to have max dimension of 512 pixels
// It scales the image while preserving aspect ratio and maintaining quality
func CompressImage(imageData []byte) ([]byte, error) {
	// Get the original orientation from EXIF data
	orientation := GetImageOrientation(imageData)

	// Decode the image
	img, _, err := image.Decode(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// Apply orientation correction if needed
	if orientation != 1 {
		img = CorrectImageOrientation(img, orientation)
		log.Infof("Applied orientation correction: %d", orientation)
	}

	// Get original dimensions
	bounds := img.Bounds()
	originalWidth := bounds.Dx()
	originalHeight := bounds.Dy()

	// Check if image needs compression
	if originalWidth <= maxImageDimension && originalHeight <= maxImageDimension {
		// Image is already within limits, return as-is
		return imageData, nil
	}

	// Calculate scale to fit within maxImageDimension while preserving aspect ratio
	scaleX := float64(maxImageDimension) / float64(originalWidth)
	scaleY := float64(maxImageDimension) / float64(originalHeight)
	scale := scaleX
	if scaleY < scaleX {
		scale = scaleY
	}

	// Calculate new dimensions
	newWidth := int(float64(originalWidth) * scale)
	newHeight := int(float64(originalHeight) * scale)

	// Ensure dimensions don't exceed the limit
	if newWidth > maxImageDimension {
		newWidth = maxImageDimension
	}
	if newHeight > maxImageDimension {
		newHeight = maxImageDimension
	}

	// Create a new image with calculated dimensions
	newImg := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Scale the image while preserving orientation and aspect ratio
	draw.ApproxBiLinear.Scale(newImg, newImg.Bounds(), img, img.Bounds(), draw.Over, nil)

	// Encode with constant quality
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, newImg, &jpeg.Options{Quality: minImageQuality})
	if err != nil {
		return nil, fmt.Errorf("failed to encode compressed image: %w", err)
	}

	compressedData := buf.Bytes()
	log.Infof("Image compressed: %d bytes -> %d bytes (quality: %d, scale: %.2f, original: %dx%d, new: %dx%d, orientation: %d)",
		len(imageData), len(compressedData), minImageQuality, scale, originalWidth, originalHeight, newWidth, newHeight, orientation)

	return compressedData, nil
}
