# Face Detector Service

A Python web service for detecting and blurring faces in images using base64 encoding.

## Features

- Base64 image processing endpoint for face detection and blurring
- Face detection using MTCNN
- Face blurring with configurable blur strength
- RESTful API design
- Comprehensive environment-based configuration
- Health check and monitoring endpoints
- Utility functions for image format conversion
- Modular image processing architecture

## Project Structure

```
face-detector/
├── app.py              # Main Flask application with endpoints
├── process_image.py    # Core image processing logic
├── detect_face.py      # Face detection using MTCNN
├── blur_faces.py       # Face blurring functionality
├── utils.py            # Image format conversion utilities
├── config.py           # Configuration management
└── requirements.txt    # Python dependencies
```

## Endpoints

- `GET /health` - Health check endpoint with configuration details
- `GET /config` - Get current configuration (excluding sensitive data)
- `GET /api/status` - API status and feature information
- `POST /process-base64` - Process base64 encoded image and return blurred version

## Utility Functions

The service includes utility functions for image processing:

- **`base64_to_numpy()`** - Convert base64 encoded images to numpy arrays (OpenCV BGR format)
- **`numpy_to_base64()`** - Convert numpy arrays back to base64 strings
- **`validate_image_array()`** - Validate numpy arrays represent valid images
- **`get_image_info()`** - Get detailed information about image arrays

### Image Format Support

- **Input formats**: JPEG, PNG, GIF, BMP (with base64 encoding)
- **Output format**: BGR numpy arrays (OpenCV compatible)
- **Automatic conversion**: RGB→BGR, RGBA→BGR, Grayscale→BGR

## Configuration

The service uses environment variables for all configuration. Copy `env.example` to `.env` and customize:

```bash
cp env.example .env
# Edit .env with your settings
```

### Key Configuration Categories

#### Flask Configuration
- `FLASK_ENV` - Environment (development/production)
- `DEBUG` - Enable debug mode
- `PORT` - Service port (default: 5000)
- `HOST` - Bind address (default: 0.0.0.0)

#### Image Processing
- `MAX_IMAGE_SIZE` - Maximum image size in bytes (default: 10MB)
- `ALLOWED_IMAGE_TYPES` - Comma-separated list of allowed image types
- `IMAGE_QUALITY` - JPEG quality for processed images

#### Face Detection
- `FACE_DETECTION_CONFIDENCE` - Confidence threshold for face detection
- `BLUR_STRENGTH` - Blur intensity for detected faces

#### PII Detection
- `PII_DETECTION_ENABLED` - Enable/disable PII detection
- `PII_DETECTION_CONFIDENCE` - Confidence threshold for PII detection

#### OpenAI Integration
- `OPENAI_API_KEY` - Your OpenAI API key
- `OPENAI_MODEL` - Model to use for PII detection

#### Security & Rate Limiting
- `CORS_ENABLED` - Enable CORS support
- `RATE_LIMIT_ENABLED` - Enable rate limiting
- `RATE_LIMIT_REQUESTS` - Max requests per window
- `RATE_LIMIT_WINDOW` - Time window in seconds

## Setup

1. Create a virtual environment:
```bash
python -m venv venv
source venv/bin/activate  # On Windows: venv\Scripts\activate
```

2. Install dependencies:
```bash
pip install -r requirements.txt
```

3. Configure environment:
```bash
cp env.example .env
# Edit .env with your settings
```

4. Run the service:
```bash
python app.py
```

The service will run on the configured host and port (default: `http://localhost:5000`).

## Usage

### Process Base64 Image

Send a POST request to `/process-base64` with JSON body:

```json
{
  "image": "base64_encoded_image_string_here"
}
```

The service will:
1. Decode the base64 image
2. Detect faces using MTCNN
3. Blur detected face regions
4. Return the processed image as base64

### Response Format

```json
{
  "message": "Base64 image processed successfully",
  "estimated_size": 12345,
  "faces_detected": 2,
  "original_image": "base64_encoded_original_image",
  "processed_image": "base64_encoded_blurred_image",
  "image_info": {
    "shape": [480, 640, 3],
    "dtype": "uint8",
    "size": 921600,
    "channels": 3
  },
  "status": "completed"
}
```

## Docker

Build and run with Docker:

```bash
# Build image
docker build -t face-detector .

# Run container
docker run -p 5000:5000 --env-file .env face-detector

# Or use docker-compose
docker-compose up --build
```

## Development

The service is now fully functional with:
- Face detection using MTCNN
- Face blurring with configurable strength
- Base64 image processing
- Comprehensive error handling and logging

## Configuration Validation

The service validates configuration on startup and provides detailed health checks. Check the `/health` endpoint for configuration status and `/config` for current settings.

## Dependencies

- Flask - Web framework
- Pillow - Image processing
- OpenCV - Computer vision library
- NumPy - Numerical computing
- python-dotenv - Environment variable management
- MTCNN - Face detection