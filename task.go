package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"

	"github.com/hibiken/asynq"
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

func HandleCreateArtistSquareTask(ctx context.Context, t *asynq.Task) error {
	var p CreateArtistSquarePayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		logger.Errorf("Job %s: Failed to unmarshal payload: %v", p.JobID, err)
		return fmt.Errorf("json.Unmarshal failed: %v: %w", err, asynq.SkipRetry)
	}

	logger.Infof("Job %s: Starting artist square creation for %d images", p.JobID, len(p.ImageURLs))

	squarePath := filepath.Join(artistSquares, fmt.Sprintf("%s.jpg", p.Key))

	if _, err := os.Stat(squarePath); err == nil {
		logger.Infof("Job %s: Artist square already exists for key %s", p.JobID, p.Key)
		return nil
	}

	logger.Infof("Job %s: Downloading images", p.JobID)
	images, err := downloadImages(p.ImageURLs)
	if err != nil {
		logger.Errorf("Job %s: Failed to download images: %v", p.JobID, err)
		return fmt.Errorf("failed to download images: %w", err)
	}
	logger.Infof("Job %s: Successfully downloaded %d images", p.JobID, len(images))

	logger.Infof("Job %s: Creating artist square", p.JobID)
	square, err := createArtistSquare(images)
	if err != nil {
		logger.Errorf("Job %s: Failed to create artist square: %v", p.JobID, err)
		return fmt.Errorf("failed to create artist square: %w", err)
	}
	logger.Infof("Job %s: Successfully created artist square", p.JobID)

	logger.Infof("Job %s: Saving JPEG to %s", p.JobID, squarePath)
	if err := saveJPEG(square, squarePath); err != nil {
		logger.Errorf("Job %s: Failed to save artist square: %v", p.JobID, err)
		return fmt.Errorf("failed to save artist square: %w", err)
	}

	logger.Infof("Job %s: Artist square created and saved successfully for key %s", p.JobID, p.Key)
	return nil
}

func downloadImages(urls []string) ([]image.Image, error) {
	images := make([]image.Image, len(urls))
	for i, url := range urls {
		logger.Infof("Downloading image from URL: %s", url)
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
		logger.Infof("Successfully downloaded and decoded image from URL: %s", url)
	}
	return images, nil
}

func createArtistSquare(images []image.Image) (image.Image, error) {
	size := 500
	background := image.NewRGBA(image.Rect(0, 0, size, size))

	switch len(images) {
	case 2:
		draw.Draw(background, image.Rect(0, 0, size/2, size), resize.Resize(uint(size/2), uint(size), images[0], resize.Lanczos3), image.Point{}, draw.Src)
		draw.Draw(background, image.Rect(size/2, 0, size, size), resize.Resize(uint(size/2), uint(size), images[1], resize.Lanczos3), image.Point{}, draw.Src)
	case 3:
		draw.Draw(background, image.Rect(0, 0, size, size/2), resize.Resize(uint(size), uint(size/2), images[0], resize.Lanczos3), image.Point{}, draw.Src)
		draw.Draw(background, image.Rect(0, size/2, size/2, size), resize.Resize(uint(size/2), uint(size/2), images[1], resize.Lanczos3), image.Point{}, draw.Src)
		draw.Draw(background, image.Rect(size/2, size/2, size, size), resize.Resize(uint(size/2), uint(size/2), images[2], resize.Lanczos3), image.Point{}, draw.Src)
	case 4:
		draw.Draw(background, image.Rect(0, 0, size/2, size/2), resize.Resize(uint(size/2), uint(size/2), images[0], resize.Lanczos3), image.Point{}, draw.Src)
		draw.Draw(background, image.Rect(size/2, 0, size, size/2), resize.Resize(uint(size/2), uint(size/2), images[1], resize.Lanczos3), image.Point{}, draw.Src)
		draw.Draw(background, image.Rect(0, size/2, size/2, size), resize.Resize(uint(size/2), uint(size/2), images[2], resize.Lanczos3), image.Point{}, draw.Src)
		draw.Draw(background, image.Rect(size/2, size/2, size, size), resize.Resize(uint(size/2), uint(size/2), images[3], resize.Lanczos3), image.Point{}, draw.Src)
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

func HandleCreateICloudArtTask(ctx context.Context, t *asynq.Task) error {
	var p CreateICloudArtPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("json.Unmarshal failed: %v: %w", err, asynq.SkipRetry)
	}

	logger.Infof("Job %s: Starting iCloud art creation for image %s", p.JobID, p.ImageURL)

	iCloudPath := filepath.Join(icloudArt, fmt.Sprintf("%s.jpg", p.Key))

	if _, err := os.Stat(iCloudPath); err == nil {
		logger.Infof("Job %s: iCloud art already exists for key %s", p.JobID, p.Key)
		return nil
	}

	img, err := downloadImage(p.ImageURL)
	if err != nil {
		return fmt.Errorf("failed to download image: %w", err)
	}

	iCloudImg, err := createICloudArt(img)
	if err != nil {
		return fmt.Errorf("failed to create iCloud art: %w", err)
	}

	if err := saveJPEG(iCloudImg, iCloudPath); err != nil {
		return fmt.Errorf("failed to save iCloud art: %w", err)
	}

	logger.Infof("Job %s: iCloud art created for key %s", p.JobID, p.Key)
	return nil
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
