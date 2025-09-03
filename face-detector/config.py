import os
from dotenv import load_dotenv

# Load environment variables from .env file if it exists
load_dotenv()

class Config:
    """Configuration class that reads all values from environment variables"""
    
    # FastAPI Server Configuration
    DEBUG = os.getenv('DEBUG', 'false').lower() == 'true'
    PORT = int(os.getenv('PORT', 8080))
    HOST = os.getenv('HOST', '0.0.0.0')
    RELOAD = os.getenv('RELOAD', 'false').lower() == 'true'
    ACCESS_LOG = os.getenv('ACCESS_LOG', 'true').lower() == 'true'
    
    # Image Processing Configuration
    MAX_IMAGE_SIZE = int(os.getenv('MAX_IMAGE_SIZE', 10 * 1024 * 1024))  # 10MB default
    
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
        
        # Validate port range
        if not (1 <= cls.PORT <= 65535):
            raise ValueError(f"PORT must be between 1 and 65535, got {cls.PORT}")
        
        # Validate image size
        if cls.MAX_IMAGE_SIZE <= 0:
            raise ValueError(f"MAX_IMAGE_SIZE must be positive, got {cls.MAX_IMAGE_SIZE}")
        
        # Validate blur strength
        if cls.BLUR_STRENGTH <= 0:
            raise ValueError(f"BLUR_STRENGTH must be positive, got {cls.BLUR_STRENGTH}")
        
        # Validate log level
        valid_log_levels = ['DEBUG', 'INFO', 'WARNING', 'ERROR', 'CRITICAL']
        if cls.LOG_LEVEL not in valid_log_levels:
            raise ValueError(f"LOG_LEVEL must be one of {valid_log_levels}, got {cls.LOG_LEVEL}")
        
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
