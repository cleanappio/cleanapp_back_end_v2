import os
from dotenv import load_dotenv

# Load environment variables from .env file if it exists
load_dotenv()

class Config:
    """Configuration class that reads all values from environment variables"""
    
    # Flask Configuration
    FLASK_ENV = os.getenv('FLASK_ENV', 'production')
    DEBUG = os.getenv('DEBUG', 'false').lower() == 'true'
    PORT = int(os.getenv('PORT', 8080))
    HOST = os.getenv('HOST', '0.0.0.0')
    
    # Image Processing Configuration
    MAX_IMAGE_SIZE = int(os.getenv('MAX_IMAGE_SIZE', 10 * 1024 * 1024))  # 10MB default
    OUTPUT_IMAGE_FORMAT = os.getenv('OUTPUT_IMAGE_FORMAT', 'JPEG')  # JPEG or PNG
    OUTPUT_IMAGE_QUALITY = int(os.getenv('OUTPUT_IMAGE_QUALITY', 95))  # 1-100 for JPEG
    
    # Face Detection Configuration
    BLUR_STRENGTH = int(os.getenv('BLUR_STRENGTH', 15))
    
    # Logging Configuration
    LOG_LEVEL = os.getenv('LOG_LEVEL', 'INFO')
    LOG_FORMAT = os.getenv('LOG_FORMAT', '%(asctime)s - %(name)s - %(levelname)s - %(message)s')
    
    # Health Check Configuration
    HEALTH_CHECK_ENABLED = os.getenv('HEALTH_CHECK_ENABLED', 'true').lower() == 'true'
    
    @classmethod
    def validate(cls):
        """Validate required configuration values"""
        required_vars = []
        
        # Add validation logic here if needed
        # For example, check if required API keys are present
        
        if required_vars:
            missing_vars = [var for var in required_vars if not getattr(cls, var)]
            if missing_vars:
                raise ValueError(f"Missing required environment variables: {', '.join(missing_vars)}")
        
        return True
    
    @classmethod
    def to_dict(cls):
        """Convert configuration to dictionary (excluding sensitive data)"""
        config_dict = {}
        
        for key, value in cls.__dict__.items():
            if not key.startswith('_'):
                config_dict[key] = value
        
        return config_dict
