import base64
import requests
import json

# Make POST request to the API
rep_seq = [
    44641,
    58851
]
rep_ids = [
    '0xF9d867F445c3003099bAFd04F56F26DC12d7F1fe',
    '0xdA4F93664617C91E48A2373af1FA6f8866D9B1c6'
]
url = "http://api.cleanapp.io:8080/read_report"
for rep_id, seq in zip(rep_ids, rep_seq):
  data = {
      "version": "2.0",
      "id": rep_id,
      "seq": seq
  }

  try:
      response = requests.post(url, json=data)
      response.raise_for_status()  # Raise an exception for bad status codes
      
      # Parse the JSON response
      response_data = response.json()
      
      # Extract the base64 string from the "image" field
      base64_string = response_data.get("image")
      
      if base64_string:
          # Decode and save the image
          with open(f"/Users/eko2000/Downloads/img_{seq}.jpg", "wb") as f:
              f.write(base64.b64decode(base64_string))
          print(f"Image successfully saved to /Users/eko2000/Downloads/img_{seq}.jpg")
      else:
          print("No image data found in the response")
          
  except requests.exceptions.RequestException as e:
      print(f"Error making request: {e}")
  except json.JSONDecodeError as e:
      print(f"Error parsing JSON response: {e}")
  except Exception as e:
      print(f"An unexpected error occurred: {e}")
