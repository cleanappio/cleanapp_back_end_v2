import os
from dotenv import load_dotenv

# Load environment variables from .env file if it exists
load_dotenv()

class Config:
    """Configuration class that reads all values from environment variables"""
    
    # Flask Configuration
    FLASK_ENV = os.getenv('FLASK_ENV', 'production')
    DEBUG = os.getenv('DEBUG', 'false').lower() == 'true'
    PORT = int(os.getenv('PORT', 5000))
    HOST = os.getenv('HOST', '0.0.0.0')
    
    # API Configuration
    API_VERSION = os.getenv('API_VERSION', 'v1')
    API_PREFIX = os.getenv('API_PREFIX', '/api')
    
    # Image Processing Configuration
    MAX_IMAGE_SIZE = int(os.getenv('MAX_IMAGE_SIZE', 10 * 1024 * 1024))  # 10MB default
    ALLOWED_IMAGE_TYPES = os.getenv('ALLOWED_IMAGE_TYPES', 'jpg,jpeg,png,gif,bmp').split(',')
    IMAGE_QUALITY = int(os.getenv('IMAGE_QUALITY', 85))
    
    # Face Detection Configuration
    FACE_DETECTION_MODEL_PATH = os.getenv('FACE_DETECTION_MODEL_PATH', '')
    FACE_DETECTION_CONFIDENCE = float(os.getenv('FACE_DETECTION_CONFIDENCE', 0.5))
    BLUR_STRENGTH = int(os.getenv('BLUR_STRENGTH', 15))
    
    # PII Detection Configuration
    PII_DETECTION_ENABLED = os.getenv('PII_DETECTION_ENABLED', 'true').lower() == 'true'
    PII_DETECTION_CONFIDENCE = float(os.getenv('PII_DETECTION_CONFIDENCE', 0.7))
    
    # OpenAI Configuration (if using OpenAI for PII detection)
    OPENAI_API_KEY = os.getenv('OPENAI_API_KEY', '')
    OPENAI_MODEL = os.getenv('OPENAI_MODEL', 'gpt-4-vision-preview')
    OPENAI_MAX_TOKENS = int(os.getenv('OPENAI_MAX_TOKENS', 1000))
    
    # Logging Configuration
    LOG_LEVEL = os.getenv('LOG_LEVEL', 'INFO')
    LOG_FORMAT = os.getenv('LOG_FORMAT', '%(asctime)s - %(name)s - %(levelname)s - %(message)s')
    
    # Security Configuration
    CORS_ENABLED = os.getenv('CORS_ENABLED', 'false').lower() == 'true'
    CORS_ORIGINS = os.getenv('CORS_ORIGINS', '*').split(',')
    RATE_LIMIT_ENABLED = os.getenv('RATE_LIMIT_ENABLED', 'false').lower() == 'true'
    RATE_LIMIT_REQUESTS = int(os.getenv('RATE_LIMIT_REQUESTS', 100))
    RATE_LIMIT_WINDOW = int(os.getenv('RATE_LIMIT_WINDOW', 3600))  # 1 hour
    
    # Storage Configuration
    UPLOAD_FOLDER = os.getenv('UPLOAD_FOLDER', '/tmp/uploads')
    MAX_UPLOAD_FILES = int(os.getenv('MAX_UPLOAD_FILES', 10))
    
    # Health Check Configuration
    HEALTH_CHECK_ENABLED = os.getenv('HEALTH_CHECK_ENABLED', 'true').lower() == 'true'
    HEALTH_CHECK_TIMEOUT = int(os.getenv('HEALTH_CHECK_TIMEOUT', 30))
    
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
        sensitive_keys = ['OPENAI_API_KEY']
        
        for key, value in cls.__dict__.items():
            if not key.startswith('_') and key not in sensitive_keys:
                config_dict[key] = value
        
        return config_dict
