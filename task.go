package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"

	"github.com/nfnt/resize"
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

func downloadImages(urls []string) ([]image.Image, error) {
	images := make([]image.Image, len(urls))
	for i, url := range urls {
		resp, err := http.Get(url)
		if err != nil {
			return nil, fmt.Errorf("failed to download image %s: %w", url, err)
		}
		defer resp.Body.Close()

		var img image.Image
		if filepath.Ext(url) == ".webp" {
			img, err = webp.Decode(resp.Body)
		} else {
			img, _, err = image.Decode(resp.Body)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to decode image %s: %w", url, err)
		}
		images[i] = img
	}
	return images, nil
}

func createArtistSquare(images []image.Image) (image.Image, error) {
	size := 500
	background := image.NewRGBA(image.Rect(0, 0, size, size))

	resizeAndDraw := func(img image.Image, rect image.Rectangle) {
		// Calculate aspect ratio
		srcAspect := float64(img.Bounds().Dx()) / float64(img.Bounds().Dy())
		dstAspect := float64(rect.Dx()) / float64(rect.Dy())

		var resizedImg image.Image
		if srcAspect > dstAspect {
			// Image is wider, resize based on height
			newHeight := uint(rect.Dy())
			newWidth := uint(float64(newHeight) * srcAspect)
			resizedImg = resize.Resize(newWidth, newHeight, img, resize.Lanczos3)
		} else {
			// Image is taller, resize based on width
			newWidth := uint(rect.Dx())
			newHeight := uint(float64(newWidth) / srcAspect)
			resizedImg = resize.Resize(newWidth, newHeight, img, resize.Lanczos3)
		}

		// Calculate positioning to center the image
		srcBounds := resizedImg.Bounds()
		dx := (srcBounds.Dx() - rect.Dx()) / 2
		dy := (srcBounds.Dy() - rect.Dy()) / 2
		draw.Draw(background, rect, resizedImg, image.Point{dx, dy}, draw.Src)
	}

	switch len(images) {
	case 2:
		resizeAndDraw(images[0], image.Rect(0, 0, size/2, size))
		resizeAndDraw(images[1], image.Rect(size/2, 0, size, size))
	case 3:
		resizeAndDraw(images[0], image.Rect(0, 0, size, size/2))
		resizeAndDraw(images[1], image.Rect(0, size/2, size/2, size))
		resizeAndDraw(images[2], image.Rect(size/2, size/2, size, size))
	case 4:
		resizeAndDraw(images[0], image.Rect(0, 0, size/2, size/2))
		resizeAndDraw(images[1], image.Rect(size/2, 0, size, size/2))
		resizeAndDraw(images[2], image.Rect(0, size/2, size/2, size))
		resizeAndDraw(images[3], image.Rect(size/2, size/2, size, size))
	default:
		return nil, fmt.Errorf("unsupported number of images: %d", len(images))
	}

	return background, nil
}

func saveJPEG(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
}

func downloadImage(url string) (image.Image, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download image %s: %w", url, err)
	}
	defer resp.Body.Close()

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image %s: %w", url, err)
	}

	return img, nil
}

func createICloudArt(img image.Image) (image.Image, error) {
	size := 1024
	return resize.Resize(uint(size), uint(size), img, resize.Lanczos3), nil
}
