package main

import (
	"encoding/json"
	"fmt"
	"os"

	"cpqd.com.br/alfred/internal/extra"
)

type Config struct {
	Manager      Manager       `json:"manager"`
	Environments []Environment `json:"environments"`
}

type Manager struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type Default struct {
	language string
}

type Environment struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Url      string `json:"url"`
	Defaults Default
}

func OpenConfiguration(fpath string) (*Config, error) {
	output := &Config{}

	info, err := os.Stat(fpath)
	if err != nil {
		return output, nil
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

	return validate_and_return(output)
}

func validate_and_return(config *Config) (*Config, error) {
	if len(config.Manager.Host) == 0 {
		return nil, fmt.Errorf("Missing entry 'manager.host'")
	}
	if config.Manager.Port == 0 {
		return nil, fmt.Errorf("Missing entry 'manager.port'")
	}

	if len(config.Environments) == 0 {
		return nil, fmt.Errorf("At least one environment is required")
	}

	for i, env := range config.Environments {
		if len(env.Name) == 0 {
			return nil, fmt.Errorf("Missing name for environment #%d", i)
		}
		if len(env.Path) == 0 {
			return nil, fmt.Errorf("Missing path for environment '%s'", env.Name)
		}
		info, err := os.Stat(env.Path)
		if err != nil {
			return nil, fmt.Errorf("Unable to stat path (%s) for environment '%s'", env.Path, env.Name)
		} else if !info.IsDir() {
			return nil, fmt.Errorf("'%s' must be a regular directory", env.Path)
		}

		if len(env.Host) == 0 {
			return nil, fmt.Errorf("Missing 'host' for environment '%s'", env.Name)
		}
		if env.Port == 0 {
			return nil, fmt.Errorf("Missing 'port' for environment '%s'", env.Name)
		}
		if len(env.Url) == 0 {
			return nil, fmt.Errorf("Missing 'url' for environment '%s'", env.Name)
		}
	}

	return config, nil
}
