package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
)

type Config struct {
	ShortLivedToken string `json:"shortLivedToken"`
	LongLivedToken  string `json:"longLivedToken"`
}

func getConfig() (*Config, error) {
	fileContent, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		} else {
			return nil, fmt.Errorf("read token failed: %w", err)
		}
	}
	var cfg Config
	if err := json.Unmarshal(fileContent, &cfg); err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	return &cfg, nil
}

func storeConfig(cfg Config) error {
	configDir := path.Dir(configPath)
	stat, err := os.Stat(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(configDir, 0700); err != nil {
				return fmt.Errorf("create config directory failed: %w", err)
			}
		}
	} else {
		if !stat.IsDir() {
			return fmt.Errorf("config path is not a directory")
		}
	}
	f, err := os.OpenFile(configPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("open token file failed: %w", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("encode token failed: %w", err)
	}
	return nil
}
