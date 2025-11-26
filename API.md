# Altbot API Documentation

Generate alt-text for your images programmatically.

## Getting Access

Purchase API access on [Ko-fi](https://ko-fi.com/micr0byte). You'll receive your API key via email.

**Pricing:** $5 minimum (pay what you can afford)  
**Limits:** Up to 5,000 images per month, no rate limit

## Authentication

Include your API key in the `Authorization` header:

```
Authorization: Bearer altbot_your_api_key_here
```

## Endpoints

### Generate Alt-Text

```
POST /api/v1/alt-text
```

**Request:**
- Content-Type: `multipart/form-data`
- Body:
  - `image` (required): Image file (JPEG, PNG, GIF, WebP, BMP, TIFF)
  - `language` (optional): Language code for alt-text (default: `en`)

**Supported languages:** en, es, fr, de, it, ja, zh, ko, pt, ru, pl, and more.

**Response:**
```json
{
  "alt_text": "A photograph of a sunset over mountains with orange and purple clouds...",
  "media_type": "image",
  "language": "en"
}
```

**Error Response:**
```json
{
  "error": "Error message here",
  "status": 400
}
```

### Check Usage

```
GET /api/v1/usage
```

**Response:**
```json
{
  "usage_this_month": 42,
  "monthly_limit": 5000,
  "remaining": 4958,
  "days_remaining": 23,
  "expires_at": "2025-02-15T00:00:00Z"
}
```

### Health Check

```
GET /api/v1/health
```

**Response:**
```json
{
  "status": "healthy",
  "version": "2.2"
}
```

## Examples

### cURL

```bash
# Generate alt-text
curl -X POST https://api.altbot.example.com/api/v1/alt-text \
  -H "Authorization: Bearer altbot_your_key_here" \
  -F "image=@photo.jpg"

# With language
curl -X POST https://api.altbot.example.com/api/v1/alt-text \
  -H "Authorization: Bearer altbot_your_key_here" \
  -F "image=@photo.jpg" \
  -F "language=de"

# Check usage
curl -H "Authorization: Bearer altbot_your_key_here" \
  https://api.altbot.example.com/api/v1/usage
```

### Python

```python
import requests

API_KEY = "altbot_your_key_here"
API_URL = "https://api.altbot.example.com"

def generate_alt_text(image_path, language="en"):
    with open(image_path, "rb") as f:
        response = requests.post(
            f"{API_URL}/api/v1/alt-text",
            headers={"Authorization": f"Bearer {API_KEY}"},
            files={"image": f},
            data={"language": language}
        )
    
    if response.ok:
        return response.json()["alt_text"]
    else:
        raise Exception(response.json()["error"])

# Usage
alt_text = generate_alt_text("photo.jpg")
print(alt_text)
```

### JavaScript (Node.js)

```javascript
const fs = require('fs');
const FormData = require('form-data');
const fetch = require('node-fetch');

const API_KEY = 'altbot_your_key_here';
const API_URL = 'https://api.altbot.example.com';

async function generateAltText(imagePath, language = 'en') {
  const form = new FormData();
  form.append('image', fs.createReadStream(imagePath));
  form.append('language', language);

  const response = await fetch(`${API_URL}/api/v1/alt-text`, {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${API_KEY}`,
    },
    body: form,
  });

  const data = await response.json();
  
  if (!response.ok) {
    throw new Error(data.error);
  }
  
  return data.alt_text;
}

// Usage
generateAltText('photo.jpg')
  .then(altText => console.log(altText))
  .catch(err => console.error(err));
```

### Batch Processing (Python)

```python
import requests
from pathlib import Path
from concurrent.futures import ThreadPoolExecutor
import time

API_KEY = "altbot_your_key_here"
API_URL = "https://api.altbot.example.com"

def process_image(image_path):
    """Process a single image and return (path, alt_text) or (path, error)"""
    try:
        with open(image_path, "rb") as f:
            response = requests.post(
                f"{API_URL}/api/v1/alt-text",
                headers={"Authorization": f"Bearer {API_KEY}"},
                files={"image": f},
                timeout=120
            )
        
        if response.ok:
            return (image_path, response.json()["alt_text"], None)
        else:
            return (image_path, None, response.json()["error"])
    except Exception as e:
        return (image_path, None, str(e))

def batch_process(image_folder, max_workers=5):
    """Process all images in a folder"""
    images = list(Path(image_folder).glob("*.jpg")) + \
             list(Path(image_folder).glob("*.png"))
    
    results = {}
    
    with ThreadPoolExecutor(max_workers=max_workers) as executor:
        for path, alt_text, error in executor.map(process_image, images):
            if error:
                print(f"Error processing {path}: {error}")
            else:
                results[str(path)] = alt_text
                print(f"Processed: {path.name}")
    
    return results

# Usage
results = batch_process("./my_photos", max_workers=5)

# Save results
import json
with open("alt_texts.json", "w") as f:
    json.dump(results, f, indent=2)
```

## Error Codes

| Status | Meaning |
|--------|---------|
| 200 | Success |
| 400 | Bad request (missing image, invalid format) |
| 401 | Invalid or missing API key |
| 429 | Monthly limit exceeded |
| 500 | Server error |
| 503 | Server busy, try again |
| 504 | Request timeout |

## Limits

- **Monthly limit:** 5,000 images
- **Max file size:** 50 MB
- **Supported formats:** JPEG, PNG, GIF, WebP, BMP, TIFF
- **Timeout:** 120 seconds per request

## Privacy

- Images are processed and immediately discarded
- No image content is stored
- Only usage metadata is logged (timestamps, counts)
- Full policy: [PRIVACY.md](https://github.com/micr0-dev/Altbot/blob/main/PRIVACY.md)

## Support

Having issues? Reach out:
- Mastodon: [@altbot@fuzzies.wtf](https://fuzzies.wtf/@altbot)
- GitHub: [Issues](https://github.com/micr0-dev/Altbot/issues)