import logging
from utils import base64_to_numpy, numpy_to_base64, get_image_info
from detect_face import detect_face
from blur_faces import blur_faces
from config import Config

logger = logging.getLogger(__name__)

def process_base64_image(base64_image: str) -> dict:
    """
    Process a base64 encoded image by detecting faces and blurring them.
    
    Args:
        base64_image (str): Base64 encoded image string
        
    Returns:
        dict: Dictionary containing processing results and processed image
        
    Raises:
        ValueError: If the base64 string is invalid or empty
        Exception: If image processing fails
    """
    try:
        # Validate base64 data
        if not base64_image or len(base64_image) == 0:
            raise ValueError("Empty base64 image data")
        
        # Estimate size (base64 is ~33% larger than binary)
        estimated_size = len(base64_image) * 3 // 4
        if estimated_size > Config.MAX_IMAGE_SIZE:
            raise ValueError(f"Image too large. Maximum size: {Config.MAX_IMAGE_SIZE / (1024*1024):.1f}MB")
        
        # Convert base64 to numpy array using utility function
        numpy_image, original_bytes, original_format = base64_to_numpy(base64_image)
        logger.info(f"Successfully converted base64 to numpy array: {numpy_image.shape}, original format: {original_format}")
        
        # Get image information
        image_info = get_image_info(numpy_image)
        
        # Detect faces
        faces = detect_face(numpy_image)
        
        # Apply face blurring directly to JPEG bytes to preserve original colors
        from utils import apply_face_blurring_to_jpeg_bytes
        processed_base64 = apply_face_blurring_to_jpeg_bytes(original_bytes, faces)
        
        logger.info(f"Image processing completed: faces detected: {len(faces)}, processed image size: {len(processed_base64)} chars")
        
        # Return processing results
        return {
            "message": "Base64 image processed successfully",
            "estimated_size": estimated_size,
            "faces_detected": len(faces),
            "processed_image": processed_base64,
            "image_info": image_info,
            "status": "completed"
        }
        
    except ValueError as e:
        logger.warning(f"Validation error in image processing: {str(e)}")
        raise
    except Exception as e:
        logger.error(f"Error processing base64 image: {str(e)}")
        raise
