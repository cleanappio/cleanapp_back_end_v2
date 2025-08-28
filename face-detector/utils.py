import base64
import io
import numpy as np
from PIL import Image
import cv2
import logging

logger = logging.getLogger(__name__)

def base64_to_numpy(base64_string: str) -> np.ndarray:
    """
    Convert a base64 encoded image string to a numpy array.
    
    Args:
        base64_string (str): Base64 encoded image string
        
    Returns:
        np.ndarray: Numpy array representing the image (BGR format for OpenCV compatibility)
        
    Raises:
        ValueError: If the base64 string is invalid or empty
        Exception: If image processing fails
    """
    try:
        # Validate input
        if not base64_string or len(base64_string.strip()) == 0:
            raise ValueError("Base64 string is empty or None")
        
        # Remove data URL prefix if present (e.g., "data:image/jpeg;base64,")
        if base64_string.startswith('data:'):
            base64_string = base64_string.split(',', 1)[1]
        
        # Decode base64 string
        try:
            image_data = base64.b64decode(base64_string)
        except Exception as e:
            raise ValueError(f"Invalid base64 string: {str(e)}")
        
        # Convert to PIL Image
        try:
            pil_image = Image.open(io.BytesIO(image_data))
        except Exception as e:
            raise ValueError(f"Failed to decode image data: {str(e)}")
        
        # Convert PIL Image to numpy array
        # OpenCV uses BGR format, so we need to convert from RGB
        numpy_array = np.array(pil_image)
        
        # Handle different image modes
        if len(numpy_array.shape) == 3:
            if numpy_array.shape[2] == 3:  # RGB image
                # Convert RGB to BGR for OpenCV compatibility
                numpy_array = cv2.cvtColor(numpy_array, cv2.COLOR_RGB2BGR)
            elif numpy_array.shape[2] == 4:  # RGBA image
                # Convert RGBA to BGR (drop alpha channel)
                numpy_array = cv2.cvtColor(numpy_array, cv2.COLOR_RGBA2BGR)
        elif len(numpy_array.shape) == 2:  # Grayscale image
            # Convert grayscale to BGR (3-channel)
            numpy_array = cv2.cvtColor(numpy_array, cv2.COLOR_GRAY2BGR)
        
        logger.debug(f"Successfully converted base64 image to numpy array with shape: {numpy_array.shape}")
        return numpy_array
        
    except Exception as e:
        logger.error(f"Error converting base64 to numpy array: {str(e)}")
        raise

def numpy_to_base64(numpy_array: np.ndarray, image_format: str = 'JPEG', quality: int = 95) -> str:
    """
    Convert a numpy array to a base64 encoded string.
    
    Args:
        numpy_array (np.ndarray): Numpy array representing the image
        image_format (str): Image format (JPEG, PNG, etc.)
        quality (int): Image quality for JPEG (1-100)
        
    Returns:
        str: Base64 encoded image string
        
    Raises:
        ValueError: If the numpy array is invalid
        Exception: If image processing fails
    """
    try:
        # Validate input
        if numpy_array is None or numpy_array.size == 0:
            raise ValueError("Numpy array is empty or None")
        
        # Convert BGR to RGB if needed (OpenCV format to PIL format)
        if len(numpy_array.shape) == 3 and numpy_array.shape[2] == 3:
            # Convert BGR to RGB for PIL
            rgb_array = cv2.cvtColor(numpy_array, cv2.COLOR_BGR2RGB)
        else:
            rgb_array = numpy_array
        
        # Convert to PIL Image
        pil_image = Image.fromarray(rgb_array)
        
        # Save to bytes buffer
        buffer = io.BytesIO()
        
        if image_format.upper() == 'JPEG':
            pil_image.save(buffer, format='JPEG', quality=quality, optimize=True)
        else:
            pil_image.save(buffer, format=image_format)
        
        # Get bytes and encode to base64
        image_bytes = buffer.getvalue()
        base64_string = base64.b64encode(image_bytes).decode('utf-8')
        
        logger.debug(f"Successfully converted numpy array to base64 string with format: {image_format}")
        return base64_string
        
    except Exception as e:
        logger.error(f"Error converting numpy array to base64: {str(e)}")
        raise

def validate_image_array(numpy_array: np.ndarray) -> bool:
    """
    Validate that a numpy array represents a valid image.
    
    Args:
        numpy_array (np.ndarray): Numpy array to validate
        
    Returns:
        bool: True if valid, False otherwise
    """
    try:
        if numpy_array is None:
            return False
        
        # Check if array has valid dimensions
        if len(numpy_array.shape) < 2 or len(numpy_array.shape) > 3:
            return False
        
        # Check if array has valid size
        if numpy_array.size == 0:
            return False
        
        # Check if array has valid data type
        if not np.issubdtype(numpy_array.dtype, np.number):
            return False
        
        # Check if array has valid values (not all NaN or inf)
        if np.any(np.isnan(numpy_array)) or np.any(np.isinf(numpy_array)):
            return False
        
        return True
        
    except Exception:
        return False

def get_image_info(numpy_array: np.ndarray) -> dict:
    """
    Get information about a numpy array representing an image.
    
    Args:
        numpy_array (np.ndarray): Numpy array representing the image
        
    Returns:
        dict: Dictionary containing image information
    """
    try:
        if not validate_image_array(numpy_array):
            return {"error": "Invalid image array"}
        
        info = {
            "shape": numpy_array.shape,
            "dtype": str(numpy_array.dtype),
            "size": numpy_array.size,
            "min_value": float(np.min(numpy_array)),
            "max_value": float(np.max(numpy_array)),
            "channels": numpy_array.shape[2] if len(numpy_array.shape) == 3 else 1
        }
        
        return info
        
    except Exception as e:
        return {"error": str(e)}
