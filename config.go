package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Port        int    `yaml:"port"`
		HostKeyPath string `yaml:"host_key_path"`
	} `yaml:"server"`

	Storage struct {
		Driver string `yaml:"driver"`
		S3     struct {
			Bucket    string `yaml:"bucket"`
			Region    string `yaml:"region"`
			Endpoint  string `yaml:"endpoint"`
			AccessKey string `yaml:"access_key"`
			SecretKey string `yaml:"secret_key"`
		} `yaml:"s3"`
	} `yaml:"storage"`

	Webhook struct {
		URL    string `yaml:"url"`
		Secret string `yaml:"secret"`
	} `yaml:"webhook"`

	Users []struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"users"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
