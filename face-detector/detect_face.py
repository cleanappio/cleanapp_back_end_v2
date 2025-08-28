import numpy as np
import logging
from mtcnn import MTCNN
from mtcnn.utils.images import load_image

logger = logging.getLogger(__name__)

def detect_face(numpy_image: np.ndarray) -> list[dict]:
    """
    Detect faces in the image and return their bounding boxes.
    
    Args:
        numpy_image (np.ndarray): Input image as numpy array (RGB format)
        
    Returns:
        list[dict]: List of detected faces with bounding box information
    """
    try:
        # Create a detector instance
        detector = MTCNN(device="CPU:0")

        # MTCNN expects RGB images, so we can pass the numpy array directly
        # since we're now keeping images in RGB format
        result = detector.detect_faces(numpy_image)
        
        logger.info(f"Detected {len(result)} faces in image")
        return result
        
    except Exception as e:
        logger.error(f"Error in face detection: {str(e)}")
        # Return empty list on error
        return []
