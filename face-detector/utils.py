import base64
import io
import logging
import numpy as np
import cv2

logger = logging.getLogger(__name__)

def base64_to_numpy(base64_string: str) -> tuple[np.ndarray, bytes, str]:
    """
    Convert a base64 encoded image string to a numpy array using OpenCV directly.
    
    Args:
        base64_string (str): Base64 encoded image string
        
    Returns:
        tuple[np.ndarray, bytes, str]: (numpy array, original image bytes, original format)
        
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
        
        # Convert to numpy array using OpenCV directly
        # This avoids PIL color space conversion issues
        numpy_array = cv2.imdecode(np.frombuffer(image_data, np.uint8), cv2.IMREAD_COLOR)
        
        if numpy_array is None:
            raise ValueError("Failed to decode image data with OpenCV")
        
        # OpenCV reads images as BGR, convert to RGB for consistency
        numpy_array = cv2.cvtColor(numpy_array, cv2.COLOR_BGR2RGB)
        
        # Detect format from the original bytes
        # For simplicity, assume JPEG since that's what you specified
        original_format = 'JPEG'
        
        logger.debug(f"Successfully converted base64 image to numpy array with shape: {numpy_array.shape}")
        return numpy_array, image_data, original_format
        
    except Exception as e:
        logger.error(f"Error converting base64 image to numpy array: {str(e)}")
        raise

def numpy_to_base64(numpy_array: np.ndarray, image_format: str = 'JPEG', quality: int = 100, original_bytes: bytes = None) -> str:
    """
    Convert a numpy array to a base64 encoded string using OpenCV directly.
    
    Args:
        numpy_array (np.ndarray): Numpy array representing the image
        image_format (str): Image format (JPEG, PNG, etc.)
        quality (int): Image quality for JPEG (1-100)
        original_bytes (bytes): Original image bytes (not used for output, just for reference)
        
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
        
        # Ensure the array is in BGR format for OpenCV
        if len(numpy_array.shape) == 3 and numpy_array.shape[2] == 3:
            # If the array is RGB (from our processing), convert to BGR for OpenCV
            if numpy_array.dtype == np.uint8:
                # Convert RGB to BGR for OpenCV encoding
                bgr_array = cv2.cvtColor(numpy_array, cv2.COLOR_RGB2BGR)
            else:
                bgr_array = numpy_array
        else:
            bgr_array = numpy_array
        
        # Encode using OpenCV directly
        if image_format.upper() == 'JPEG':
            # Encode as JPEG with specified quality
            encode_params = [cv2.IMWRITE_JPEG_QUALITY, quality]
            success, encoded_image = cv2.imencode('.jpg', bgr_array, encode_params)
        elif image_format.upper() == 'PNG':
            # Encode as PNG (lossless)
            success, encoded_image = cv2.imencode('.png', bgr_array)
        else:
            # Default to JPEG
            encode_params = [cv2.IMWRITE_JPEG_QUALITY, quality]
            success, encoded_image = cv2.imencode('.jpg', bgr_array, encode_params)
        
        if not success:
            raise Exception(f"Failed to encode image as {image_format}")
        
        # Convert to base64
        image_bytes = encoded_image.tobytes()
        base64_string = base64.b64encode(image_bytes).decode('utf-8')
        
        logger.debug(f"Successfully converted numpy array to base64 string with format: {image_format}")
        return base64_string
        
    except Exception as e:
        logger.error(f"Error converting numpy array to base64: {str(e)}")
        raise

def apply_face_blurring_to_jpeg_bytes(original_bytes: bytes, faces: list) -> str:
    """
    Apply face blurring directly to JPEG bytes to preserve original colors.
    
    Args:
        original_bytes (bytes): Original JPEG image bytes
        faces (list): List of detected faces to blur
        
    Returns:
        str: Base64 encoded JPEG with blurred faces
        
    Raises:
        Exception: If face blurring fails
    """
    try:
        # Decode JPEG bytes to numpy array
        numpy_array = cv2.imdecode(np.frombuffer(original_bytes, np.uint8), cv2.IMREAD_COLOR)
        
        if numpy_array is None:
            raise Exception("Failed to decode JPEG bytes")
        
        # Convert BGR to RGB for processing
        rgb_array = cv2.cvtColor(numpy_array, cv2.COLOR_BGR2RGB)
        
        # Apply face blurring
        from blur_faces import blur_faces
        blurred_array = blur_faces(rgb_array, faces)
        
        # Convert back to BGR for JPEG encoding
        bgr_array = cv2.cvtColor(blurred_array, cv2.COLOR_RGB2BGR)
        
        # Encode back to JPEG with maximum quality
        encode_params = [cv2.IMWRITE_JPEG_QUALITY, 100]
        success, encoded_image = cv2.imencode('.jpg', bgr_array, encode_params)
        
        if not success:
            raise Exception("Failed to encode blurred image as JPEG")
        
        # Convert to base64
        image_bytes = encoded_image.tobytes()
        base64_string = base64.b64encode(image_bytes).decode('utf-8')
        
        logger.debug("Successfully applied face blurring to JPEG bytes")
        return base64_string
        
    except Exception as e:
        logger.error(f"Error applying face blurring to JPEG bytes: {str(e)}")
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
