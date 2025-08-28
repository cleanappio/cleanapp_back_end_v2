import numpy as np

from mtcnn import MTCNN
from mtcnn.utils.images import load_image


def detect_face(image_data: np.ndarray) -> list[dict]:
    """
    Detect faces in the image and return their bounding boxes.
    """
    # Create a detector instance
    detector = MTCNN(device="CPU:0")

    # Load an image
    image = load_image(image_data)

    # Detect faces in the image
    result = detector.detect_faces(image)

    return result
