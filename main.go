package main

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

var (
	logger        *logrus.Logger
	cacheDir      string
	artistSquares string
	icloudArt     string
	animatedArt   string
)

func init() {
	// Initialize logger
	logger = logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Get the directory of the executable
	ex, err := os.Executable()
	if err != nil {
		logger.Fatalf("Error getting executable path: %v", err)
	}
	exPath := filepath.Dir(ex)
	logger.Infof("Executable path: %s", exPath)

	// Set up directories with absolute paths
	cacheDir = filepath.Join(exPath, "cache")
	artistSquares = filepath.Join(cacheDir, "artist-squares")
	icloudArt = filepath.Join(cacheDir, "icloud-art")
	animatedArt = filepath.Join(cacheDir, "animated-art")

	logger.Infof("Cache directory: %s", cacheDir)
	logger.Infof("Artist squares directory: %s", artistSquares)
	logger.Infof("iCloud art directory: %s", icloudArt)
	logger.Infof("Animated art directory: %s", animatedArt)

	ffmpeg.LogCompiledCommand = false

	ensureDirectories()
}

func ensureDirectories() {
	dirs := []string{cacheDir, artistSquares, icloudArt, animatedArt}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			logger.Errorf("Error creating directory %s: %v", dir, err)
		}
	}
}

func generateKey(url string) string {
	hash := md5.Sum([]byte(url))
	return hex.EncodeToString(hash[:])
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Routes
	r.GET("/artwork/generate", generateArtwork)
	r.GET("/artwork/:key", getArtwork)
	r.POST("/artwork/artist-square", generateArtistSquare)
	r.GET("/artwork/artist-square/:key", getArtistSquare)
	r.POST("/artwork/icloud", generateICloudArt)
	r.GET("/artwork/icloud/:key", getICloudArt)

	// Start server
	if err := r.Run(":3000"); err != nil {
		logger.Fatal("Failed to start server: ", err)
	}
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

func getArtwork(c *gin.Context) {
	key := strings.TrimSuffix(c.Param("key"), ".gif")
	gifPath := filepath.Join(animatedArt, fmt.Sprintf("%s.gif", key))

	if _, err := os.Stat(gifPath); os.IsNotExist(err) {
		logger.Warnf("GIF not found for key: %s", key)
		c.JSON(http.StatusNotFound, gin.H{"error": "GIF not found"})
		return
	} else if err != nil {
		logger.Errorf("Error accessing GIF for key %s: %v", key, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error accessing GIF"})
		return
	}

	c.File(gifPath)
}

func generateArtistSquareKey(imageUrls []string) string {
	sort.Strings(imageUrls)
	combinedUrls := strings.Join(imageUrls, "")
	hash := md5.Sum([]byte(combinedUrls))
	return hex.EncodeToString(hash[:])
}

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
		err := generateArtistSquareAsync(request.ImageURLs, key, squarePath)
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

func generateArtistSquareAsync(imageURLs []string, key, squarePath string) error {
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

	if err := saveJPEG(square, squarePath); err != nil {
		logger.Errorf("Failed to save artist square: %v", err)
		return fmt.Errorf("failed to save artist square: %w", err)
	}

	return nil
}

func getArtistSquare(c *gin.Context) {
	key := strings.TrimSuffix(c.Param("key"), ".jpg")
	squarePath := filepath.Join(artistSquares, fmt.Sprintf("%s.jpg", key))

	if _, err := os.Stat(squarePath); os.IsNotExist(err) {
		logger.Warnf("Artist Square not found for key: %s", key)
		c.JSON(http.StatusNotFound, gin.H{"error": "Artist Square not found"})
		return
	} else if err != nil {
		logger.Errorf("Error accessing Artist Square for key %s: %v", key, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error accessing Artist Square"})
		return
	}

	c.File(squarePath)
}

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
	iCloudPath := filepath.Join(icloudArt, fmt.Sprintf("%s.jpg", key))

	if _, err := os.Stat(iCloudPath); err == nil {
		c.JSON(http.StatusOK, gin.H{
			"key":     key,
			"message": "iCloud art already exists",
			"url":     fmt.Sprintf("https://art.cider.sh/artwork/icloud/%s.jpg", key),
		})
		return
	}

	// Create a channel to receive the result
	resultChan := make(chan error)

	// Start a goroutine to generate the iCloud art
	go func() {
		err := generateICloudArtAsync(request.ImageURL, key, iCloudPath)
		resultChan <- err
	}()

	// Wait for the goroutine to complete or timeout
	select {
	case err := <-resultChan:
		if err != nil {
			logger.Errorf("Failed to generate iCloud art: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate iCloud art"})
		} else {
			c.JSON(http.StatusOK, gin.H{
				"key":     key,
				"message": "iCloud art has been generated",
				"url":     fmt.Sprintf("https://art.cider.sh/artwork/icloud/%s.jpg", key),
			})
		}
	case <-time.After(30 * time.Second): // Adjust timeout as needed
		c.JSON(http.StatusAccepted, gin.H{
			"key":     key,
			"message": "iCloud art is still being processed. Please check back later.",
			"url":     fmt.Sprintf("https://art.cider.sh/artwork/icloud/%s.jpg", key),
		})
	}
}

func generateICloudArtAsync(imageURL, key, iCloudPath string) error {
	img, err := downloadImage(imageURL)
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

	logger.Infof("iCloud art created and saved successfully for key %s", key)
	return nil
}

func getICloudArt(c *gin.Context) {
	key := strings.TrimSuffix(c.Param("key"), ".jpg")
	iCloudPath := filepath.Join(icloudArt, fmt.Sprintf("%s.jpg", key))

	if _, err := os.Stat(iCloudPath); os.IsNotExist(err) {
		logger.Warnf("iCloud Art not found for key: %s", key)
		c.JSON(http.StatusNotFound, gin.H{"error": "iCloud Art not found"})
		return
	} else if err != nil {
		logger.Errorf("Error accessing iCloud Art for key %s: %v", key, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error accessing iCloud Art"})
		return
	}

	c.File(iCloudPath)
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
