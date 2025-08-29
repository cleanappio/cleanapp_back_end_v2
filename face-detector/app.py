from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse
from pydantic import BaseModel
import logging
import os
from config import Config
from process_image import process_base64_image

# Configure logging based on environment
logging.basicConfig(
    level=getattr(logging, Config.LOG_LEVEL),
    format=Config.LOG_FORMAT
)
logger = logging.getLogger(__name__)

# Create FastAPI app
app = FastAPI(
    title="Face Detector Service",
    description="A service for detecting and blurring faces in images",
    version="1.0.0"
)

# Pydantic model for request validation
class ImageRequest(BaseModel):
    image: str

# Pydantic model for response
class ImageResponse(BaseModel):
    message: str
    estimated_size: int
    faces_detected: int
    processed_image: str
    image_info: dict
    status: str

@app.get('/health')
async def health_check():
    """Health check endpoint"""
    if not Config.HEALTH_CHECK_ENABLED:
        raise HTTPException(status_code=503, detail="health_check_disabled")
    
    try:
        # Basic health checks
        health_status = {
            "status": "healthy",
            "service": "face-detector",
            "config": Config.to_dict()
        }
        
        # Validate configuration
        Config.validate()
        
        return health_status
    except Exception as e:
        logger.error(f"Health check failed: {str(e)}")
        raise HTTPException(status_code=503, detail=f"Health check failed: {str(e)}")

@app.get('/config')
async def get_config():
    """Get current configuration (excluding sensitive data)"""
    return Config.to_dict()

@app.post('/process-base64', response_model=ImageResponse)
async def process_base64_image_endpoint(request: ImageRequest):
    """
    Process base64 encoded image endpoint - detects faces and returns blurred image
    """
    try:
        base64_image = request.image
        
        # Process the image using the dedicated function
        try:
            result = process_base64_image(base64_image)
            return ImageResponse(**result)
            
        except ValueError as e:
            raise HTTPException(status_code=400, detail=f"Invalid image data: {str(e)}")
        except Exception as e:
            logger.error(f"Error processing base64 image: {str(e)}")
            raise HTTPException(status_code=500, detail="Failed to process image data")
        
    except HTTPException:
        raise
    except Exception as e:
        logger.error(f"Error in process-base64 endpoint: {str(e)}")
        raise HTTPException(status_code=500, detail="Internal server error")

@app.get('/api/status')
async def api_status():
    """API status endpoint"""
    return {
        "service": "face-detector",
        "status": "operational",
        "features": {
            "face_detection": True,
            "image_processing": True,
            "base64_support": True,
            "face_blurring": True
        },
        "limits": {
            "max_image_size": Config.MAX_IMAGE_SIZE
        }
    }

@app.get('/')
async def root():
    """Root endpoint with service information"""
    return {
        "service": "face-detector",
        "version": "1.0.0",
        "description": "Face detection and blurring service",
        "endpoints": {
            "health": "/health",
            "config": "/config",
            "process_image": "/process-base64",
            "status": "/api/status",
            "docs": "/docs"
        }
    }

if __name__ == '__main__':
    try:
        # Validate configuration before starting
        Config.validate()
        
        logger.info(f"Starting face-detector service on {Config.HOST}:{Config.PORT}")
        logger.info(f"Debug mode: {Config.DEBUG}")
        
        import uvicorn
        uvicorn.run(
            "app:app",
            host=Config.HOST,
            port=Config.PORT,
            reload=Config.DEBUG,  # Use DEBUG for reload in development
            log_level=Config.LOG_LEVEL.lower()
        )
    except Exception as e:
        logger.error(f"Failed to start service: {str(e)}")
        exit(1)
