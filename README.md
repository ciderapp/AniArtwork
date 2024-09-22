# AniArt

This service is part of the Cider Collective ecosystem, designed to handle various artwork-related operations for [Cider](https://cider.sh).

## Features

- Generate animated artwork from Apple Music URLs
- Create artist squares from multiple image URLs
- Process and store iCloud artwork

## API Endpoints

### 1. Generate Animated Artwork

```
GET /artwork/generate
```

Query Parameters:
- `url`: The Apple Music URL for the artwork (required)

Response:
```json
{
  "key": "unique_identifier",
  "message": "GIF has been generated",
  "url": "https://art.cider.sh/artwork/unique_identifier.gif"
}
```

### 2. Create Artist Square

```
POST /artwork/artist-square
```

Request Body:
```json
{
  "imageUrls": ["url1", "url2", "url3", "url4"]
}
```
Note: 2-4 image URLs are required.

Response:
```json
{
  "key": "unique_identifier",
  "message": "Artist square has been generated",
  "url": "https://art.cider.sh/artwork/artist-square/unique_identifier.jpg"
}
```

### 3. Generate iCloud Artwork

```
POST /artwork/icloud
```

Request Body:
```json
{
  "imageUrl": "url"
}
```

Response:
```json
{
  "key": "unique_identifier",
  "message": "iCloud art has been generated",
  "url": "https://art.cider.sh/artwork/icloud/unique_identifier.ext"
}
```

### 4. Retrieve Artwork

- Animated Artwork: `GET /artwork/:key`
- Artist Square: `GET /artwork/artist-square/:key`
- iCloud Artwork: `GET /artwork/icloud/:key`

## Setup and Deployment

1. Ensure you have Go installed on your system.
2. Clone this repository.
3. Install dependencies: `go mod tidy`
4. Build the project: `go build`
5. Run the server: `./AniArt`

The server will start on port 3000 by default.

## Dependencies

- github.com/gin-gonic/gin
- github.com/sirupsen/logrus
- github.com/u2takey/ffmpeg-go
- github.com/nfnt/resize
- golang.org/x/image/webp

## License

Copyright © 2024 Cider Collective

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Support

For support, please open an issue on the GitHub repository or contact the Cider Collective team.