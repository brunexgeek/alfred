package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"cpqd.com.br/alfred/internal/catalog"
	"cpqd.com.br/alfred/internal/extra"
)

type Config struct {
	Manager      Manager                `json:"manager"`
	Environments []*catalog.Environment `json:"environments"`
}

type Manager struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func OpenConfiguration(fpath string) (*Config, error) {
	output := &Config{}

	info, err := os.Stat(fpath)
	if err != nil {
		return nil, err
	} else if info.IsDir() {
		return nil, fmt.Errorf("'%s' must be a regular file", fpath)
	}

	file, err := os.OpenFile(fpath, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := extra.ReadAll(file, 5*1024)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data, output)
	if err != nil {
		return nil, err
	}

	return validate_and_return(fpath, output)
}

func validate_and_return(cpath string, config *Config) (*Config, error) {
	if len(config.Manager.Host) == 0 {
		return nil, fmt.Errorf("missing entry 'manager.host'")
	}
	if config.Manager.Port == 0 {
		return nil, fmt.Errorf("missing entry 'manager.port'")
	}

	if len(config.Environments) == 0 {
		return nil, fmt.Errorf("at least one environment is required")
	}

	for i, env := range config.Environments {
		if len(env.Name) == 0 {
			return nil, fmt.Errorf("missing name for environment #%d", i)
		}
		if len(env.Path) == 0 {
			return nil, fmt.Errorf("missing path for environment '%s'", env.Name)
		}
		info, err := os.Stat(env.Path)
		if err != nil {
			return nil, fmt.Errorf("unable to stat path (%s) for environment '%s'", env.Path, env.Name)
		} else if !info.IsDir() {
			return nil, fmt.Errorf("'%s' must be a regular directory", env.Path)
		}

		if len(env.Url) == 0 {
			return nil, fmt.Errorf("missing 'url' for environment '%s'", env.Name)
		}
		env.Url = strings.TrimSuffix(env.Url, "/")
	}

	return config, nil
}
