package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration
type Config struct {
	Title      string `yaml:"title"`
	Content    string `yaml:"content"`
	AppID      string `yaml:"appid"`
	Secret     string `yaml:"secret"`
	UserID     string `yaml:"userid"`
	TemplateID string `yaml:"template_id"`
	BaseURL    string `yaml:"base_url"`
	Timezone   string `yaml:"tz"`
	Port       string `yaml:"port"`
}

// LoadConfig reads configuration from a YAML file
func LoadConfig(filename string) (*Config, error) {
	// Initialize with defaults
	config := &Config{
		Timezone: "Asia/Shanghai",
		BaseURL:  "https://push.hzz.cool",
		Port:     "5566",
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// If config file doesn't exist, return defaults
			return config, nil
		}
		return nil, err
	}

	err = yaml.Unmarshal(data, config)
	if err != nil {
		return nil, err
	}

	return config, nil
}
