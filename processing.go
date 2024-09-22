package main

import (
	"fmt"
	"image"
	"image/draw"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nfnt/resize"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

/*
 * Animated Artwork Processing
 *
 * /POST /artwork/generate
 */

func generateArtworkAsync(urlStr, key, gifPath string) error {
	tempGifPath := filepath.Join(animatedArt, fmt.Sprintf("%s_temp.gif", key))

	defer func() {
		if _, err := os.Stat(tempGifPath); err == nil {
			logger.Infof("Cleaning up temporary file %s", tempGifPath)
			if err := os.Remove(tempGifPath); err != nil {
				logger.Errorf("Failed to remove temporary file %s: %v", tempGifPath, err)
			}
		}
	}()

	// Parse the m3u8 file
	streamURL, err := getHighQualityStreamURL(urlStr)
	if err != nil {
		return fmt.Errorf("failed to get high quality stream URL: %w", err)
	}

	err = ffmpeg.Input(streamURL).
		Output(tempGifPath, ffmpeg.KwArgs{
			"vf":                "scale=486:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse",
			"loop":              "0", // Loop infinitely
			"threads":           "8",
			"preset":            "fast",
			"multiple_requests": "1",
			"buffer_size":       "8192k",
			"loglevel":          "panic", // Only log errors
		}).
		GlobalArgs("-hide_banner").
		OverWriteOutput().
		ErrorToStdOut().
		Run()

	if err != nil {
		logger.Errorf("FFmpeg error: %v", err)
		return fmt.Errorf("ffmpeg command failed: %w", err)
	}

	if fi, err := os.Stat(tempGifPath); err != nil || fi.Size() == 0 {
		logger.Errorf("Temporary file %s was not created or is empty", tempGifPath)
		return fmt.Errorf("ffmpeg failed to create output file")
	}

	if err := os.Rename(tempGifPath, gifPath); err != nil {
		logger.Errorf("Error renaming file: %v", err)
		return fmt.Errorf("error renaming file: %w", err)
	}

	return nil
}

func generateArtwork(c *gin.Context) {
	urlStr := c.Query("url")
	if urlStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "URL query parameter is required"})
		return
	}

	if err := isValidAppleURL(urlStr); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	key := generateKey(urlStr)
	gifPath := filepath.Join(animatedArt, fmt.Sprintf("%s.gif", key))

	if _, err := os.Stat(gifPath); err == nil {
		c.JSON(http.StatusOK, gin.H{
			"key":     key,
			"message": "GIF already exists",
			"url":     fmt.Sprintf("https://art.cider.sh/artwork/%s.gif", key),
		})
		return
	}

	resultChan := make(chan error)

	go func() {
		err := generateArtworkAsync(urlStr, key, gifPath)
		resultChan <- err
	}()

	select {
	case err := <-resultChan:
		if err != nil {
			logger.Errorf("Failed to generate artwork: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate artwork"})
		} else {
			c.JSON(http.StatusOK, gin.H{
				"key":     key,
				"message": "GIF has been generated",
				"url":     fmt.Sprintf("https://art.cider.sh/artwork/%s.gif", key),
			})
		}
	case <-time.After(30 * time.Second):
		c.JSON(http.StatusInternalServerError, gin.H{"error": "GIF generation timed out"})
	}
}

/*
 * Artist Square Processing
 *
 * /POST /artwork/create_artist_square
 */

func generateArtistSquare(c *gin.Context) {
	var request struct {
		ImageURLs []string `json:"imageUrls" binding:"required,min=2,max=4"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	for _, url := range request.ImageURLs {
		if err := isValidAppleURL(url); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid URL: %s. %s", url, err.Error())})
			return
		}
	}

	key := generateArtistSquareKey(request.ImageURLs)
	squarePath := filepath.Join(artistSquares, fmt.Sprintf("%s.jpg", key))

	if _, err := os.Stat(squarePath); err == nil {
		c.JSON(http.StatusOK, gin.H{
			"key":     key,
			"message": "Artist square already exists",
			"url":     fmt.Sprintf("https://art.cider.sh/artwork/artist-square/%s.jpg", key),
		})
		return
	}

	// Create a channel to receive the result
	resultChan := make(chan error)

	// Start a goroutine to generate the artist square
	go func() {
		err := generateArtistSquareAsync(request.ImageURLs, key)
		resultChan <- err
	}()

	// Wait for the goroutine to complete or timeout
	select {
	case err := <-resultChan:
		if err != nil {
			logger.Errorf("Failed to generate artist square: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate artist square"})
		} else {
			c.JSON(http.StatusOK, gin.H{
				"key":     key,
				"message": "Artist square has been generated",
				"url":     fmt.Sprintf("https://art.cider.sh/artwork/artist-square/%s.jpg", key),
			})
		}
	case <-time.After(30 * time.Second): // Adjust timeout as needed
		c.JSON(http.StatusAccepted, gin.H{
			"key":     key,
			"message": "Artist square is still being processed. Please check back later.",
			"url":     fmt.Sprintf("https://art.cider.sh/artwork/artist-square/%s.jpg", key),
		})
	}
}

func generateArtistSquareAsync(imageURLs []string, key string) error {
	images, err := downloadImages(imageURLs)
	if err != nil {
		logger.Errorf("Failed to download images: %v", err)
		return fmt.Errorf("failed to download images: %w", err)
	}

	square, err := createArtistSquare(images)
	if err != nil {
		logger.Errorf("Failed to create artist square: %v", err)
		return fmt.Errorf("failed to create artist square: %w", err)
	}

	squarePath := filepath.Join(artistSquares, fmt.Sprintf("%s.jpg", key))

	if err := saveImage(square, squarePath, "jpg"); err != nil {
		logger.Errorf("Failed to save artist square: %v", err)
		return fmt.Errorf("failed to save artist square: %w", err)
	}

	return nil
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

/*
 * iCloud Art Processing
 *
 * /POST /artwork/create_icloud_art
 */

func generateICloudArt(c *gin.Context) {
	var request struct {
		ImageURL string `json:"imageUrl" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := isValidAppleURL(request.ImageURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	key := generateKey(request.ImageURL)

	// Check if the image already exists in any of the supported formats
	formats := []string{"jpg", "jpeg", "png", "gif"}
	var existingPath string
	for _, format := range formats {
		testPath := filepath.Join(icloudArt, fmt.Sprintf("%s.%s", key, format))
		if _, err := os.Stat(testPath); err == nil {
			existingPath = testPath
			break
		}
	}

	if existingPath != "" {
		// Image already exists, return its information
		c.JSON(http.StatusOK, gin.H{
			"key":     key,
			"message": "iCloud art already exists",
			"url":     fmt.Sprintf("https://art.cider.sh/artwork/icloud/%s%s", key, filepath.Ext(existingPath)),
		})
		return
	}

	// Image doesn't exist, generate it
	resultChan := make(chan error)

	go func() {
		err := generateICloudArtAsync(request.ImageURL, key)
		resultChan <- err
	}()

	select {
	case err := <-resultChan:
		if err != nil {
			logger.Errorf("Failed to generate iCloud art: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate iCloud art"})
		} else {
			// Find the generated file and its format
			var generatedPath string
			for _, format := range formats {
				testPath := filepath.Join(icloudArt, fmt.Sprintf("%s.%s", key, format))
				if _, err := os.Stat(testPath); err == nil {
					generatedPath = testPath
					break
				}
			}

			if generatedPath == "" {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to locate generated iCloud art"})
			} else {
				c.JSON(http.StatusOK, gin.H{
					"key":     key,
					"message": "iCloud art has been generated",
					"url":     fmt.Sprintf("https://art.cider.sh/artwork/icloud/%s%s", key, filepath.Ext(generatedPath)),
				})
			}
		}
	case <-time.After(30 * time.Second): // Adjust timeout as needed
		c.JSON(http.StatusAccepted, gin.H{
			"key":     key,
			"message": "iCloud art is still being processed. Please check back later.",
			"url":     fmt.Sprintf("https://art.cider.sh/artwork/icloud/%s", key),
		})
	}
}

func generateICloudArtAsync(imageURL, key string) error {
	img, format, err := downloadImage(imageURL)
	if err != nil {
		return fmt.Errorf("failed to download image: %w", err)
	}

	iCloudImg, err := createICloudArt(img)
	if err != nil {
		return fmt.Errorf("failed to create iCloud art: %w", err)
	}

	// Use the original format for the file extension
	iCloudPath := filepath.Join(icloudArt, fmt.Sprintf("%s.%s", key, format))

	if err := saveImage(iCloudImg, iCloudPath, format); err != nil {
		return fmt.Errorf("failed to save iCloud art: %w", err)
	}

	return nil
}

func createICloudArt(img image.Image) (image.Image, error) {
	size := 1024
	return resize.Resize(uint(size), uint(size), img, resize.Lanczos3), nil
}
