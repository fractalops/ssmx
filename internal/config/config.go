package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

const configFileName = "config"
const configFileType = "yaml"

// Dir returns the path to the ssmx config directory (~/.ssmx).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".ssmx"), nil
}

// Load reads ~/.ssmx/config.yaml, returning an empty Config if the file does
// not exist yet.
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetConfigName(configFileName)
	v.SetConfigType(configFileType)
	v.AddConfigPath(dir)

	// Set defaults so an empty/missing file still returns a usable struct.
	v.SetDefault("aliases", map[string]string{})
	v.SetDefault("doc_aliases", DefaultDocAliases)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
		// Config file doesn't exist yet — return defaults.
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// Save writes cfg to ~/.ssmx/config.yaml, creating the directory if needed.
func Save(cfg *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	v := viper.New()
	v.SetConfigName(configFileName)
	v.SetConfigType(configFileType)
	v.AddConfigPath(dir)

	v.Set("default_profile", cfg.DefaultProfile)
	v.Set("default_region", cfg.DefaultRegion)
	v.Set("aliases", cfg.Aliases)
	v.Set("doc_aliases", cfg.DocAliases)

	path := filepath.Join(dir, configFileName+"."+configFileType)
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// SetAlias adds or updates an alias → instance ID mapping and saves.
func SetAlias(name, instanceID string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	if cfg.Aliases == nil {
		cfg.Aliases = make(map[string]string)
	}
	cfg.Aliases[name] = instanceID
	return Save(cfg)
}

// RemoveAlias deletes an alias by name and saves.
func RemoveAlias(name string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	delete(cfg.Aliases, name)
	return Save(cfg)
}
