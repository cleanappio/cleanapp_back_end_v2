#!/usr/bin/env python3
"""
Test script for the face-detector FastAPI service.
Reads an image file, converts it to base64, sends it to the service, and saves the processed result.
"""

import base64
import sys
import os
import requests
import logging
from pathlib import Path

# Configure logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)

def image_to_base64(image_path: str) -> str:
    """Convert an image file to base64 encoded string."""
    try:
        with open(image_path, 'rb') as image_file:
            image_data = image_file.read()
            base64_string = base64.b64encode(image_data).decode('utf-8')
            logger.info(f"Converted image to base64: {len(base64_string)} characters")
            return base64_string
    except Exception as e:
        logger.error(f"Error converting image to base64: {e}")
        raise

def send_to_service(base64_image: str, service_url: str = "http://localhost:8080") -> dict:
    """Send base64 image to the face-detector service."""
    try:
        url = f"{service_url}/process-base64"
        payload = {"image": base64_image}
        
        logger.info(f"Sending request to: {url}")
        response = requests.post(url, json=payload, timeout=60)
        
        if response.status_code == 200:
            result = response.json()
            logger.info(f"Service response: {result.get('message', 'No message')}")
            logger.info(f"Faces detected: {result.get('faces_detected', 0)}")
            return result
        else:
            logger.error(f"Service error: {response.status_code} - {response.text}")
            raise Exception(f"Service returned status {response.status_code}")
            
    except requests.exceptions.RequestException as e:
        logger.error(f"Request failed: {e}")
        raise
    except Exception as e:
        logger.error(f"Error sending to service: {e}")
        raise

def save_processed_image(base64_image: str, output_path: str):
    """Save base64 encoded image to file."""
    try:
        image_data = base64.b64decode(base64_image)
        with open(output_path, 'wb') as output_file:
            output_file.write(image_data)
        logger.info(f"Processed image saved to: {output_path}")
    except Exception as e:
        logger.error(f"Error saving processed image: {e}")
        raise

def test_fastapi_endpoints(service_url: str = "http://localhost:8080"):
    """Test various FastAPI endpoints."""
    try:
        logger.info("Testing FastAPI endpoints...")
        
        # Test root endpoint
        response = requests.get(f"{service_url}/")
        if response.status_code == 200:
            logger.info("✅ Root endpoint working")
            logger.info(f"   Service: {response.json().get('service')}")
            logger.info(f"   Version: {response.json().get('version')}")
        else:
            logger.error(f"❌ Root endpoint failed: {response.status_code}")
        
        # Test health endpoint
        response = requests.get(f"{service_url}/health")
        if response.status_code == 200:
            logger.info("✅ Health endpoint working")
            logger.info(f"   Status: {response.json().get('status')}")
        else:
            logger.error(f"❌ Health endpoint failed: {response.status_code}")
        
        # Test config endpoint
        response = requests.get(f"{service_url}/config")
        if response.status_code == 200:
            logger.info("✅ Config endpoint working")
            config = response.json()
            logger.info(f"   Port: {config.get('PORT')}")
            logger.info(f"   Log Level: {config.get('LOG_LEVEL')}")
        else:
            logger.error(f"❌ Config endpoint failed: {response.status_code}")
        
        # Test status endpoint
        response = requests.get(f"{service_url}/api/status")
        if response.status_code == 200:
            logger.info("✅ Status endpoint working")
            status = response.json()
            logger.info(f"   Service: {status.get('service')}")
            logger.info(f"   Status: {status.get('status')}")
        else:
            logger.error(f"❌ Status endpoint failed: {response.status_code}")
        
        logger.info("FastAPI endpoint testing completed!")
        
    except Exception as e:
        logger.error(f"Error testing FastAPI endpoints: {e}")

def main():
    """Main function to test the face-detector service."""
    if len(sys.argv) < 2:
        print("Usage: python test_service.py <image_path> [service_url]")
        print("Example: python test_service.py test_image.jpg http://localhost:8080")
        sys.exit(1)
    
    image_path = sys.argv[1]
    service_url = sys.argv[2] if len(sys.argv) > 2 else "http://localhost:8080"
    
    # Validate image file
    if not os.path.exists(image_path):
        logger.error(f"Image file not found: {image_path}")
        sys.exit(1)
    
    try:
        logger.info(f"Testing face-detector service with image: {image_path}")
        logger.info(f"Service URL: {service_url}")
        
        # Test FastAPI endpoints first
        test_fastapi_endpoints(service_url)
        
        # Convert image to base64
        base64_image = image_to_base64(image_path)
        
        # Send to service
        result = send_to_service(base64_image, service_url)
        
        # Save processed image
        if 'processed_image' in result:
            # Generate output filename
            input_path = Path(image_path)
            output_path = input_path.parent / f"{input_path.stem}_processed{input_path.suffix}"
            
            save_processed_image(result['processed_image'], str(output_path))
            logger.info(f"✅ Test completed successfully!")
            logger.info(f"   Input: {image_path}")
            logger.info(f"   Output: {output_path}")
            logger.info(f"   Faces detected: {result.get('faces_detected', 0)}")
        else:
            logger.error("No processed image in response")
            sys.exit(1)
            
    except Exception as e:
        logger.error(f"Test failed: {e}")
        sys.exit(1)

if __name__ == "__main__":
    main()
