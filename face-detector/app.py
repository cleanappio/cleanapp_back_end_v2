from flask import Flask, request, jsonify
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

# Create Flask app
app = Flask(__name__)

# Apply configuration
app.config['MAX_CONTENT_LENGTH'] = Config.MAX_IMAGE_SIZE

@app.route('/health', methods=['GET'])
def health_check():
    """Health check endpoint"""
    if not Config.HEALTH_CHECK_ENABLED:
        return jsonify({"status": "health_check_disabled"}), 503
    
    try:
        # Basic health checks
        health_status = {
            "status": "healthy",
            "service": "face-detector",
            "environment": Config.FLASK_ENV,
            "config": Config.to_dict()
        }
        
        # Validate configuration
        Config.validate()
        
        return jsonify(health_status)
    except Exception as e:
        logger.error(f"Health check failed: {str(e)}")
        return jsonify({"status": "unhealthy", "error": str(e)}), 503

@app.route('/config', methods=['GET'])
def get_config():
    """Get current configuration (excluding sensitive data)"""
    return jsonify(Config.to_dict())

@app.route('/process-base64', methods=['POST'])
def process_base64_image_endpoint():
    """
    Process base64 encoded image endpoint - detects faces and returns blurred image
    """
    try:
        data = request.get_json()
        if not data or 'image' not in data:
            return jsonify({"error": "No base64 image data provided"}), 400
        
        base64_image = data['image']
        
        # Process the image using the dedicated function
        try:
            result = process_base64_image(base64_image)
            return jsonify(result)
            
        except ValueError as e:
            return jsonify({"error": f"Invalid image data: {str(e)}"}), 400
        except Exception as e:
            logger.error(f"Error processing base64 image: {str(e)}")
            return jsonify({"error": "Failed to process image data"}), 500
        
    except Exception as e:
        logger.error(f"Error in process-base64 endpoint: {str(e)}")
        return jsonify({"error": "Internal server error"}), 500

@app.route('/api/status', methods=['GET'])
def api_status():
    """API status endpoint"""
    return jsonify({
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
    })

if __name__ == '__main__':
    try:
        # Validate configuration before starting
        Config.validate()
        
        logger.info(f"Starting face-detector service on {Config.HOST}:{Config.PORT}")
        logger.info(f"Environment: {Config.FLASK_ENV}")
        logger.info(f"Debug mode: {Config.DEBUG}")
        
        app.run(
            host=Config.HOST,
            port=Config.PORT,
            debug=Config.DEBUG
        )
    except Exception as e:
        logger.error(f"Failed to start service: {str(e)}")
        exit(1)
