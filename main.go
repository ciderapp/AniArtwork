package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 - 15:04:05",
		DisableSorting:  false,
		ForceQuote:      false,
		DisableQuote:    true,
		ForceColors:     true,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "time",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})

	// Get the directory of the executable
	ex, err := os.Executable()
	if err != nil {
		logger.Fatalf("Error getting executable path: %v", err)
	}
	exPath := filepath.Dir(ex)

	// Set up directories with absolute paths
	cacheDir = filepath.Join(exPath, "cache")
	artistSquares = filepath.Join(cacheDir, "artist-squares")
	icloudArt = filepath.Join(cacheDir, "icloud-art")
	animatedArt = filepath.Join(cacheDir, "animated-art")

	logger.Info("AniArt priming up...")
	logger.Infof("Published URI: %s", getBaseURI())
	logger.Infof("Cache directory: %s", cacheDir)
	logger.Infof("Artist Squares directory: %s", artistSquares)
	logger.Infof("iCloud Art directory: %s", icloudArt)
	logger.Infof("Animated Art directory: %s", animatedArt)

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

func main() {
	gin.SetMode(gin.ReleaseMode)
	gin.ForceConsoleColor()
	r := gin.Default()

	// Routes
	r.GET("/artwork/generate", generateArtwork)
	r.GET("/artwork/:key", getArtwork)
	r.POST("/artwork/artist-square", generateArtistSquare)
	r.GET("/artwork/artist-square/:key", getArtistSquare)
	r.POST("/artwork/icloud", generateICloudArt)
	r.GET("/artwork/icloud/:key", getICloudArt)

	// Experimental, WEBP support.
	r.GET("/artwork/generate_alt", generateAltArtwork)

	// Start server
	if err := r.Run(":3000"); err != nil {
		logger.Fatal("Failed to start server: ", err)
	}
}

func getArtwork(c *gin.Context) {
	key := strings.TrimSuffix(strings.TrimSuffix(c.Param("key"), ".gif"), ".webp")
	gifPath := filepath.Join(animatedArt, fmt.Sprintf("%s.gif", key))
	webpPath := filepath.Join(animatedArt, fmt.Sprintf("%s.webp", key))

	if _, err := os.Stat(gifPath); os.IsNotExist(err) {
		if _, err := os.Stat(webpPath); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Artwork not found"})
			return
		} else if err != nil {
			logger.Errorf("Error accessing WEBP for key %s: %v", key, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error accessing WEBP"})
			return
		}
		c.File(webpPath)
		return
	} else if err != nil {
		logger.Errorf("Error accessing GIF for key %s: %v", key, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error accessing GIF"})
		return
	}

	c.File(gifPath)
}

func getArtistSquare(c *gin.Context) {
	key := strings.TrimSuffix(c.Param("key"), ".jpg")
	squarePath := filepath.Join(artistSquares, fmt.Sprintf("%s.jpg", key))

	if _, err := os.Stat(squarePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Artist Square not found"})
		return
	} else if err != nil {
		logger.Errorf("Error accessing Artist Square for key %s: %v", key, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error accessing Artist Square"})
		return
	}

	c.File(squarePath)
}

func getICloudArt(c *gin.Context) {
	key := c.Param("key")

	// Check for each possible format
	formats := []string{"jpg", "jpeg", "png", "gif"}
	var iCloudPath string

	for _, format := range formats {
		testPath := filepath.Join(icloudArt, fmt.Sprintf("%s.%s", key, format))
		if _, err := os.Stat(testPath); err == nil {
			iCloudPath = testPath
			break
		}
	}

	if iCloudPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "iCloud Art not found"})
		return
	}

	c.File(iCloudPath)
}
