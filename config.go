package main

import (
	"net"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	PublishedURI string `yaml:"PUBLISHED_URI"`
}

func getBaseURI() string {
	// Check for config.yml file first
	config := &Config{}
	if configFile, err := os.Open("config.yml"); err == nil {
		defer configFile.Close()
		if err := yaml.NewDecoder(configFile).Decode(config); err == nil && config.PublishedURI != "" {
			return config.PublishedURI
		}
	}

	// If config.yml doesn't exist or doesn't contain PUBLISHED_URI, check for environment variable
	if envURI := os.Getenv("PUBLISHED_URI"); envURI != "" {
		return envURI
	}

	// Default to device IP if neither config file nor environment variable is set
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					return "http://" + ipnet.IP.String()
				}
			}
		}
	}

	// Fallback to localhost if we can't determine the IP
	return "http://localhost"
}
