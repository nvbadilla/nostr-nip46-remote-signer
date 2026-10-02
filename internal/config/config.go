package config

import (
	"os"
)

// Config holds runtime options for the HTTP server.
type Config struct {
	Addr      string
	StaticDir string
	RelayURL  string
}

// FromEnv loads configuration from environment variables.
func FromEnv() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "web"
	}

	relayURL := os.Getenv("NOSTR_RELAY_URL")
	if relayURL == "" {
		relayURL = "wss://relay.nip46.com"
	}

	return Config{
		Addr:      "127.0.0.1:" + port,
		StaticDir: staticDir,
		RelayURL:  relayURL,
	}
}
