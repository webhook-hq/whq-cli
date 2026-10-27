// Package config stores the CLI's access token and API address in
// ~/.config/whq/config.json (owner-only permissions). WHQ_TOKEN and WHQ_API_URL override it.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const DefaultAPIURL = "https://app.webhookhq.dev"

type Config struct {
	Token  string `json:"token,omitempty"`
	APIURL string `json:"api_url,omitempty"`
}

// Path is ~/.config/whq/config.json; WHQ_CONFIG overrides it (tests, several accounts).
func Path() (string, error) {
	if path := os.Getenv("WHQ_CONFIG"); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "whq", "config.json"), nil
}

// Load reads the saved config and applies environment overrides.
func Load() (Config, error) {
	var cfg Config
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, err
		}
	}
	if token := os.Getenv("WHQ_TOKEN"); token != "" {
		cfg.Token = token
	}
	if url := os.Getenv("WHQ_API_URL"); url != "" {
		cfg.APIURL = url
	}
	if cfg.APIURL == "" {
		cfg.APIURL = DefaultAPIURL
	}
	return cfg, nil
}

// Save writes the config, readable only by the current user.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
