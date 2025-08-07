#!/usr/bin/env python3
"""
Test Report Generator

This script takes an image filename as a parameter and outputs a JSON report
with metadata and the image as a bytes array.
"""

import json
import sys
import os
import argparse
from pathlib import Path


def load_image_as_bytes(image_path):
    """
    Load an image file and return it as a bytes array.
    
    Args:
        image_path (str): Path to the image file
        
    Returns:
        bytes: Image data as bytes
        
    Raises:
        FileNotFoundError: If the image file doesn't exist
        IOError: If there's an error reading the file
    """
    if not os.path.exists(image_path):
        raise FileNotFoundError(f"Image file not found: {image_path}")
    
    try:
        with open(image_path, 'rb') as f:
            return f.read()
    except IOError as e:
        raise IOError(f"Error reading image file {image_path}: {e}")


def generate_report(image_path):
    """
    Generate a report JSON with the specified format.
    
    Args:
        image_path (str): Path to the image file
        
    Returns:
        dict: Report data in the specified JSON format
    """
    # Load the image as bytes
    image_bytes = load_image_as_bytes(image_path)
    
    # Create the report structure
    report = {
        "version": "2.0",
        "id": "0xdff9426058ccA89C0297fb45E0620bc052899A5c",
        "latitude": 47.320606,
        "longitude": 8.530489,
        "x": 0.5,
        "y": 0.5,
        "annotation": "Annotation Example",
        "image": list(image_bytes)  # Convert bytes to list for JSON serialization
    }
    
    return report


def main():
    """Main function to handle command line arguments and output the JSON."""
    parser = argparse.ArgumentParser(
        description="Generate a test report JSON from an image file",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python test_report.py image.jpg
  python test_report.py /path/to/image.png
        """
    )
    
    parser.add_argument(
        'image_path',
        help='Path to the image file'
    )
    
    parser.add_argument(
        '--output', '-o',
        help='Output file path (default: stdout)',
        default=None
    )
    
    parser.add_argument(
        '--pretty', '-p',
        action='store_true',
        help='Pretty print the JSON output'
    )
    
    args = parser.parse_args()
    
    try:
        # Generate the report
        report = generate_report(args.image_path)
        
        # Convert to JSON string
        if args.pretty:
            json_output = json.dumps(report, indent=2, ensure_ascii=False)
        else:
            json_output = json.dumps(report, ensure_ascii=False)
        
        # Output the result
        if args.output:
            with open(args.output, 'w', encoding='utf-8') as f:
                f.write(json_output)
            print(f"Report saved to: {args.output}")
        else:
            print(json_output)
            
    except FileNotFoundError as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)
    except IOError as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"Unexpected error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
