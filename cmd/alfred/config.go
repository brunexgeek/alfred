package main

import (
	"brunexgeek/alfred/internal/catalog"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"brunexgeek/alfred/internal/extra"
)

type Config struct {
	Manager      Manager                `json:"manager"`
	Environments []*catalog.Environment `json:"environments"`
	Location     string
}

type Manager struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func OpenConfiguration(fpath string) (*Config, error) {
	if !path.IsAbs(fpath) {
		var err error
		fpath, err = filepath.Abs(fpath)
		if err != nil {
			return nil, err
		}
	}

	output := &Config{Location: fpath}

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

func make_absolute(fpath string, root string) (string, error) {
	if len(fpath) == 0 {
		return "", nil
	}
	if !path.IsAbs(fpath) {
		fpath = filepath.Clean(path.Join(root, fpath))
	}

	info, err := os.Stat(fpath)
	if err != nil {
		return "", fmt.Errorf("unable to stat path '%s'", fpath)
	} else if info.IsDir() {
		return "", fmt.Errorf("'%s' must be a regular file", fpath)
	}

	return fpath, nil
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
		if env.Strings == nil {
			env.Strings = make(map[string]string)
		}
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

		// validate HTML templates
		root := path.Dir(config.Location)
		env.Templates.Products, err = make_absolute(env.Templates.Products, root)
		if err != nil {
			return nil, err
		}
		env.Templates.Languages, err = make_absolute(env.Templates.Languages, root)
		if err != nil {
			return nil, err
		}
		env.Templates.Versions, err = make_absolute(env.Templates.Versions, root)
		if err != nil {
			return nil, err
		}
		env.Templates.Formats, err = make_absolute(env.Templates.Formats, root)
		if err != nil {
			return nil, err
		}

		// make sure we have translations for item types
		names := map[string]string{
			"catalog":  "Catalog",
			"product":  "Product",
			"language": "Language",
			"version":  "Version",
			"format":   "Format",
		}
		for key, value := range names {
			if _, ok := env.Strings[key]; !ok {
				env.Strings[key] = value
			}
		}
	}

	return config, nil
}
