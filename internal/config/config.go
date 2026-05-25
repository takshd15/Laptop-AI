package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const appDir = ".laptop-ai"
const configFile = "config.yaml"

type Config struct {
	DataDir        string   `yaml:"data_dir"`
	LocalOnly      bool     `yaml:"local_only"`
	AllowedFolders []string `yaml:"allowed_folders"`
	ModelProvider  string   `yaml:"model_provider"`

	// Security settings
	CloudEnabled      bool   `yaml:"cloud_models_enabled"` // must be false for local_only
	EncryptionEnabled bool   `yaml:"encryption_enabled"`
	EncryptionSalt    string `yaml:"encryption_salt,omitempty"` // base64 — not secret, stored in plain
}

func defaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		DataDir:           filepath.Join(home, appDir, "data"),
		LocalOnly:         true,
		AllowedFolders:    []string{},
		ModelProvider:     "local",
		CloudEnabled:      false, // explicit default — no data leaves the device
		EncryptionEnabled: false,
	}
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory: %w", err)
	}
	return filepath.Join(home, appDir, configFile), nil
}

func Init() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot find home directory: %w", err)
	}

	dir := filepath.Join(home, appDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	dataDir := filepath.Join(home, appDir, "data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("cannot create data directory: %w", err)
	}

	cfgPath := filepath.Join(dir, configFile)
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		fmt.Println("laptop-ai already initialized at", dir)
		return nil
	}

	cfg := defaultConfig()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("cannot serialize config: %w", err)
	}

	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}

	fmt.Printf("Initialized laptop-ai at %s\n", dir)
	fmt.Printf("Config: %s\n", cfgPath)
	fmt.Printf("Data:   %s\n", dataDir)
	return nil
}

func (c *Config) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("cannot serialize config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func (c *Config) AddFolder(folder string) {
	for _, f := range c.AllowedFolders {
		if f == folder {
			return
		}
	}
	c.AllowedFolders = append(c.AllowedFolders, folder)
}

func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("not initialized — run: laptop-ai init")
		}
		return nil, fmt.Errorf("cannot read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("cannot parse config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) PrintStats(fileCount int) {
	fmt.Println("laptop-ai stats")
	fmt.Println("---------------")
	fmt.Printf("Data directory:  %s\n", c.DataDir)
	fmt.Printf("Model provider:  %s\n", c.ModelProvider)
	fmt.Printf("Local only:      %v\n", c.LocalOnly)
	fmt.Printf("Indexed files:   %d\n", fileCount)
	fmt.Printf("Indexed folders: %d\n", len(c.AllowedFolders))
	if len(c.AllowedFolders) == 0 {
		fmt.Println("  (none — run: laptop-ai index <folder>)")
	} else {
		for _, f := range c.AllowedFolders {
			fmt.Printf("  • %s\n", f)
		}
	}
}
