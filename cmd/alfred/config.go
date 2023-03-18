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

type Environment struct {
	Name string `json:"name"`
	Path string `json:"path"`
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

	return output, nil
}
