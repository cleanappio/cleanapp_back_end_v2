import cv2
import numpy as np
import logging
from config import Config

logger = logging.getLogger(__name__)

def blur_faces(numpy_image: np.ndarray, faces: list) -> np.ndarray:
    """
    Blur face regions in a numpy image based on detected face boxes.
    
    Args:
        numpy_image (np.ndarray): Input image as numpy array (BGR format)
        faces (list): List of face detection results, each containing {'box': [left, top, width, height]}
        
    Returns:
        np.ndarray: Modified image with blurred face regions
        
    Raises:
        ValueError: If the input image is invalid
        Exception: If blurring operation fails
    """
    try:
        # Validate input image
        if numpy_image is None or numpy_image.size == 0:
            raise ValueError("Input image is empty or None")
        
        if len(numpy_image.shape) != 3 or numpy_image.shape[2] != 3:
            raise ValueError("Input image must be a 3-channel BGR image")
        
        # Create a copy of the image to avoid modifying the original
        blurred_image = numpy_image.copy()
        
        # If no faces detected, return the original image
        if not faces or len(faces) == 0:
            logger.info("No faces detected, returning original image")
            return blurred_image
        
        logger.info(f"Blurring {len(faces)} detected faces")
        
        # Get blur strength from configuration
        blur_strength = Config.BLUR_STRENGTH
        
        # Process each detected face
        for i, face in enumerate(faces):
            try:
                # Extract bounding box coordinates from the 'box' element
                if 'box' not in face:
                    logger.warning(f"Face {i} missing 'box' element: {face}")
                    continue
                
                box = face['box']
                if len(box) != 4:
                    logger.warning(f"Face {i} has invalid box format: {box}")
                    continue
                
                left, top, width, height = box
                
                # Ensure coordinates are within image bounds
                left = max(0, int(left))
                top = max(0, int(top))
                width = min(int(width), blurred_image.shape[1] - left)
                height = min(int(height), blurred_image.shape[0] - top)
                
                # Skip if invalid dimensions
                if width <= 0 or height <= 0:
                    logger.warning(f"Invalid face dimensions for face {i}: {width}x{height}")
                    continue
                
                # Extract face region
                face_region = blurred_image[top:top+height, left:left+width]
                
                # Apply Gaussian blur to the face region
                # Use odd kernel size for Gaussian blur
                kernel_size = max(3, blur_strength if blur_strength % 2 == 1 else blur_strength + 1)
                sigma = blur_strength / 3.0  # Adjust sigma based on blur strength
                
                blurred_face = cv2.GaussianBlur(face_region, (kernel_size, kernel_size), sigma)
                
                # Replace the face region with blurred version
                blurred_image[top:top+height, left:left+width] = blurred_face
                
                logger.debug(f"Blurred face {i} at ({left}, {top}) with size {width}x{height}")
                
            except Exception as e:
                logger.error(f"Error processing face {i}: {str(e)}")
                continue
        
        logger.info(f"Successfully blurred {len(faces)} faces")
        return blurred_image
        
    except Exception as e:
        logger.error(f"Error in blur_faces function: {str(e)}")
        raise

def apply_pixelation_blur(numpy_image: np.ndarray, faces: list, pixel_size: int = 10) -> np.ndarray:
    """
    Apply pixelation blur to face regions (alternative to Gaussian blur).
    
    Args:
        numpy_image (np.ndarray): Input image as numpy array (BGR format)
        faces (list): List of face detection results with 'box' elements
        
    Returns:
        np.ndarray: Modified image with pixelated face regions
    """
    try:
        if numpy_image is None or numpy_image.size == 0:
            raise ValueError("Input image is empty or None")
        
        pixelated_image = numpy_image.copy()
        
        if not faces or len(faces) == 0:
            return pixelated_image
        
        for i, face in enumerate(faces):
            try:
                # Extract coordinates from the 'box' element
                if 'box' not in face:
                    continue
                
                box = face['box']
                if len(box) != 4:
                    continue
                
                left, top, width, height = box
                left = max(0, int(left))
                top = max(0, int(top))
                width = min(int(width), pixelated_image.shape[1] - left)
                height = min(int(height), pixelated_image.shape[0] - top)
                
                if width <= 0 or height <= 0:
                    continue
                
                # Extract face region
                face_region = pixelated_image[top:top+height, left:left+width]
                
                # Apply pixelation
                small = cv2.resize(face_region, (width // pixel_size, height // pixel_size), interpolation=cv2.INTER_LINEAR)
                pixelated_face = cv2.resize(small, (width, height), interpolation=cv2.INTER_NEAREST)
                
                # Replace face region
                pixelated_image[top:top+height, left:left+width] = pixelated_face
                
            except Exception as e:
                logger.error(f"Error pixelating face {i}: {str(e)}")
                continue
        
        return pixelated_image
        
    except Exception as e:
        logger.error(f"Error in apply_pixelation_blur function: {str(e)}")
        raise

def get_blur_effect_preview(numpy_image: np.ndarray, faces: list) -> dict:
    """
    Get a preview of blur effects without modifying the original image.
    
    Args:
        numpy_image (np.ndarray): Input image as numpy array
        faces (list): List of detected faces with 'box' elements
        
    Returns:
        dict: Dictionary containing blur effect information
    """
    try:
        if not faces or len(faces) == 0:
            return {"faces_detected": 0, "blur_effects": []}
        
        blur_effects = []
        for i, face in enumerate(faces):
            if 'box' in face and len(face['box']) == 4:
                left, top, width, height = face['box']
                blur_effects.append({
                    "face_id": i,
                    "position": {"left": left, "top": top, "width": width, "height": height},
                    "area": width * height,
                    "blur_strength": Config.BLUR_STRENGTH
                })
        
        return {
            "faces_detected": len(faces),
            "blur_effects": blur_effects,
            "total_blur_area": sum(effect["area"] for effect in blur_effects)
        }
        
    except Exception as e:
        logger.error(f"Error getting blur effect preview: {str(e)}")
        return {"error": str(e)}
