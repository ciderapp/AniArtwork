package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"golang.org/x/image/webp"
)

const (
	TypeGenerateArtwork    = "artwork:generate"
	TypeCreateArtistSquare = "artwork:create_artist_square"
	TypeCreateICloudArt    = "artwork:create_icloud_art"
)

type GenerateArtworkPayload struct {
	URL   string `json:"url"`
	Key   string `json:"key"`
	JobID string `json:"job_id"`
}

type CreateArtistSquarePayload struct {
	ImageURLs []string `json:"image_urls"`
	Key       string   `json:"key"`
	JobID     string   `json:"job_id"`
}

type CreateICloudArtPayload struct {
	ImageURL string `json:"image_url"`
	Key      string `json:"key"`
	JobID    string `json:"job_id"`
}

type streamInfo struct {
	averageBandwidth int
	bandwidth        int
	codecs           string
	frameRate        float64
	resolution       struct {
		width  int
		height int
	}
}

func downloadImages(urls []string) ([]image.Image, error) {
	var images []image.Image
	var errors []string

	for _, url := range urls {
		img, _, err := downloadImage(url)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to download image from %s: %v", url, err))
			continue
		}
		images = append(images, img)
	}

	if len(errors) > 0 {
		return images, fmt.Errorf("some images failed to download: %s", strings.Join(errors, "; "))
	}

	return images, nil
}

func generateKey(url string) string {
	hash := md5.Sum([]byte(url))
	return hex.EncodeToString(hash[:])
}

func generateArtistSquareKey(imageUrls []string) string {
	sort.Strings(imageUrls)
	combinedUrls := strings.Join(imageUrls, "")
	hash := md5.Sum([]byte(combinedUrls))
	return hex.EncodeToString(hash[:])
}

func getHighQualityStreamURL(masterPlaylistURL string) (string, error) {
	resp, err := http.Get(masterPlaylistURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch master playlist: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var selectedStreamURL string
	var maxWidth int
	var streamURL string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			info := parseStreamInfo(line)
			if isValidStream(info) {
				width := info.resolution.width
				if width > maxWidth {
					maxWidth = width
					streamURL = ""
				}
			}
		} else if strings.HasPrefix(line, "http") && streamURL == "" {
			streamURL = line
			if maxWidth > 0 {
				selectedStreamURL = streamURL
			}
		}
	}

	if selectedStreamURL == "" {
		return "", fmt.Errorf("no suitable stream found")
	}

	return resolveURL(masterPlaylistURL, selectedStreamURL), nil
}

func parseStreamInfo(line string) streamInfo {
	info := streamInfo{}
	parts := strings.Split(line[18:], ",")
	for _, part := range parts {
		keyValue := strings.SplitN(part, "=", 2)
		if len(keyValue) != 2 {
			continue
		}
		key := strings.TrimSpace(keyValue[0])
		value := strings.Trim(keyValue[1], "\"")
		switch key {
		case "AVERAGE-BANDWIDTH":
			info.averageBandwidth, _ = strconv.Atoi(value)
		case "BANDWIDTH":
			info.bandwidth, _ = strconv.Atoi(value)
		case "CODECS":
			info.codecs = value
		case "FRAME-RATE":
			info.frameRate, _ = strconv.ParseFloat(value, 64)
		case "RESOLUTION":
			res := strings.Split(value, "x")
			if len(res) == 2 {
				info.resolution.width, _ = strconv.Atoi(res[0])
				info.resolution.height, _ = strconv.Atoi(res[1])
			}
		}
	}
	return info
}

func isValidStream(info streamInfo) bool {
	return !strings.Contains(info.codecs, "hvc1") &&
		strings.Contains(info.codecs, "avc1") &&
		info.resolution.width >= 450
}

func resolveURL(base, relative string) string {
	baseURL, err := url.Parse(base)
	if err != nil {
		return relative
	}
	relativeURL, err := url.Parse(relative)
	if err != nil {
		return relative
	}
	return baseURL.ResolveReference(relativeURL).String()
}

func isValidAppleURL(urlStr string) error {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}

	hostname := parsedURL.Hostname()
	if !strings.HasSuffix(hostname, ".apple.com") && !strings.HasSuffix(hostname, ".mzstatic.com") {
		return fmt.Errorf("URL must be from *.apple.com or *.mzstatic.com domain")
	}

	return nil
}

func downloadImage(url string) (image.Image, string, error) {
	client := resty.New().
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(5 * time.Second).
		SetTimeout(30 * time.Second)

	resp, err := client.R().
		SetDoNotParseResponse(true).
		SetHeader("User-Agent", "AniArt/1.0").
		Get(url)

	if err != nil {
		return nil, "", fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.RawBody().Close()

	imgData, err := io.ReadAll(resp.RawBody())
	if err != nil {
		return nil, "", fmt.Errorf("failed to read image data: %w", err)
	}

	if len(imgData) == 0 {
		return nil, "", fmt.Errorf("downloaded image data is empty")
	}

	// Try to decode the image using image.Decode, which can handle multiple formats
	img, format, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		// If standard decoding fails, try specific decoders
		decoders := map[string]func(io.Reader) (image.Image, error){
			"jpg":  jpeg.Decode,
			"jpeg": jpeg.Decode,
			"png":  png.Decode,
			"gif":  gif.Decode,
			"webp": webp.Decode,
		}

		for ext, decoder := range decoders {
			if img, err = decoder(bytes.NewReader(imgData)); err == nil {
				format = ext
				break
			}
		}

		if err != nil {
			return nil, "", fmt.Errorf("failed to decode image: %w", err)
		}
	}

	// If the format is empty (shouldn't happen, but just in case), default to "png"
	if format == "" {
		format = "png"
	}

	return img, format, nil
}

func saveImage(img image.Image, filePath, format string) error {
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	switch format {
	case "jpeg", "jpg":
		return jpeg.Encode(file, img, &jpeg.Options{Quality: 95})
	case "png":
		return png.Encode(file, img)
	case "gif":
		return gif.Encode(file, img, &gif.Options{})
	default:
		return fmt.Errorf("unsupported image format: %s", format)
	}
}
