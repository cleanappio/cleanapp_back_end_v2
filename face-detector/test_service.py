#!/usr/bin/env python3
"""
Test script for the face-detector service.
Reads an image file, converts to base64, sends to service, and saves processed result.
"""

import base64
import json
import requests
import sys
import os
from pathlib import Path
import argparse

def image_to_base64(image_path: str) -> str:
    """
    Convert an image file to base64 string.
    
    Args:
        image_path (str): Path to the image file
        
    Returns:
        str: Base64 encoded image string
    """
    try:
        with open(image_path, 'rb') as image_file:
            image_data = image_file.read()
            base64_string = base64.b64encode(image_data).decode('utf-8')
            print(f"✓ Converted image to base64: {len(base64_string)} characters")
            return base64_string
    except Exception as e:
        print(f"❌ Error converting image to base64: {e}")
        sys.exit(1)

def send_to_service(base64_image: str, service_url: str = "http://localhost:8080") -> dict:
    """
    Send base64 image to the face-detector service.
    
    Args:
        base64_image (str): Base64 encoded image
        service_url (str): Service URL
        
    Returns:
        dict: Service response
    """
    try:
        payload = {
            "image": base64_image
        }
        
        print(f"📤 Sending request to {service_url}/process-base64...")
        response = requests.post(
            f"{service_url}/process-base64",
            json=payload,
            headers={"Content-Type": "application/json"},
            timeout=60
        )
        
        if response.status_code == 200:
            result = response.json()
            print(f"✅ Service response: {result['message']}")
            print(f"📊 Faces detected: {result['faces_detected']}")
            print(f"📏 Image info: {result['image_info']}")
            return result
        else:
            print(f"❌ Service error (status {response.status_code}): {response.text}")
            sys.exit(1)
            
    except requests.exceptions.ConnectionError:
        print(f"❌ Connection error: Could not connect to {service_url}")
        print("   Make sure the face-detector service is running with 'make run'")
        sys.exit(1)
    except requests.exceptions.Timeout:
        print("❌ Request timeout: Service took too long to respond")
        sys.exit(1)
    except Exception as e:
        print(f"❌ Error sending request: {e}")
        sys.exit(1)

def save_processed_image(processed_base64: str, output_path: str):
    """
    Save the processed base64 image to a file.
    
    Args:
        processed_base64 (str): Base64 encoded processed image
        output_path (str): Path to save the processed image
    """
    try:
        # Decode base64 to binary
        image_data = base64.b64decode(processed_base64)
        
        # Save to file
        with open(output_path, 'wb') as output_file:
            output_file.write(image_data)
        
        print(f"💾 Processed image saved to: {output_path}")
        
    except Exception as e:
        print(f"❌ Error saving processed image: {e}")
        sys.exit(1)

def main():
    """Main function to run the test."""
    parser = argparse.ArgumentParser(description='Test the face-detector service')
    parser.add_argument('input_image', help='Path to input image file')
    parser.add_argument('-o', '--output', help='Output path for processed image (default: processed_<input_name>)')
    parser.add_argument('-u', '--url', default='http://localhost:8080', help='Service URL (default: http://localhost:8080)')
    
    args = parser.parse_args()
    
    # Validate input file
    if not os.path.exists(args.input_image):
        print(f"❌ Input file not found: {args.input_image}")
        sys.exit(1)
    
    # Set output path
    if args.output:
        output_path = args.output
    else:
        input_path = Path(args.input_image)
        output_path = f"processed_{input_path.name}"
    
    print("🚀 Starting face-detector service test...")
    print(f"📁 Input image: {args.input_image}")
    print(f"📁 Output image: {output_path}")
    print(f"🌐 Service URL: {args.url}")
    print("-" * 50)
    
    # Step 1: Convert image to base64
    print("Step 1: Converting image to base64...")
    base64_image = image_to_base64(args.input_image)
    
    # Step 2: Send to service
    print("\nStep 2: Sending to face-detector service...")
    result = send_to_service(base64_image, args.url)
    
    # Step 3: Save processed image
    print("\nStep 3: Saving processed image...")
    save_processed_image(result['processed_image'], output_path)
    
    print("\n" + "=" * 50)
    print("🎉 Test completed successfully!")
    print(f"📊 Summary:")
    print(f"   - Input: {args.input_image}")
    print(f"   - Output: {output_path}")
    print(f"   - Faces detected: {result['faces_detected']}")
    print(f"   - Service: {args.url}")
    print("=" * 50)

if __name__ == "__main__":
    main()
