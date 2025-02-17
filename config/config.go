package config

import (
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	GptApiKey string `yaml:"gpt_api_key"`
	Port      int    `yaml:"port"`
}

func LoadConfig(filePath string) (*Config, error) {
	config := &Config{}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	yaml.Unmarshal(data, config)
	return config, nil
}
