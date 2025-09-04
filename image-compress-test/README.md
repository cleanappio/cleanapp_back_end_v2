# Image Compress Test

A simple Go program that compresses images using the `image.CompressImage()` function.

## Features

- Compresses JPEG images to a maximum dimension of 1024 pixels
- Preserves aspect ratio and applies EXIF orientation correction
- Outputs compressed images with `-compressed.jpg` suffix
- Shows compression statistics

## Usage

```bash
# Build the program
go build -o image-compress

# Compress an image
./image-compress photo.jpg
# This will create photo-compressed.jpg

# Or run directly with go
go run main.go photo.jpg
```

## Requirements

- Go 1.21 or later
- The required dependencies will be automatically downloaded when you run `go mod tidy`

## Dependencies

- `github.com/apex/log` - For logging
- `github.com/rwcarlsen/goexif` - For EXIF data extraction
- `golang.org/x/image` - For image processing

## Example

```bash
$ go run main.go sample.jpg
Original image size: 2048576 bytes
Image compressed: 2048576 bytes -> 512384 bytes (quality: 85, scale: 0.50, original: 2048x1536, new: 1024x768, orientation: 1)
Compressed image size: 512384 bytes
Compression ratio: 25.00%
Compressed image saved as: sample-compressed.jpg
```
