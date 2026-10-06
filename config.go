package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Port          int    `yaml:"port"`
		HostKeyPath   string `yaml:"host_key_path"`
		TrustedCAPath string `yaml:"trusted_ca_path"`
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
		GCS struct {
			Bucket          string `yaml:"bucket"`
			CredentialsFile string `yaml:"credentials_file"`
		} `yaml:"gcs"`
		Azure struct {
			Container   string `yaml:"container"`
			AccountName string `yaml:"account_name"`
			AccountKey  string `yaml:"account_key"`
		} `yaml:"azure"`
		Disk struct {
			BasePath string `yaml:"base_path"`
		} `yaml:"disk"`
	} `yaml:"storage"`

	PGP struct {
		Enabled        bool   `yaml:"enabled"`
		PrivateKeyPath string `yaml:"private_key_path"`
		Passphrase     string `yaml:"passphrase"`
	} `yaml:"pgp"`
	
	RabbitMQ struct {
		URL       string `yaml:"url"`        // e.g. "amqp://guest:guest@localhost:5672/"
		QueueName string `yaml:"queue_name"` // e.g. "hook22_webhooks"
		Enabled   bool   `yaml:"enabled"`
	} `yaml:"rabbitmq"`

	Webhook struct {
		URL         string `yaml:"url"`
		Secret      string `yaml:"secret"`
		Workers     int    `yaml:"workers"`
		MaxRetries  int    `yaml:"max_retries"`
		BaseBackoff int    `yaml:"base_backoff"`
	} `yaml:"webhook"`

	Users []struct {
		Username   string   `yaml:"username"`
		Password   string   `yaml:"password"`
		PublicKeys []string `yaml:"public_keys"`
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
	if cfg.Webhook.Workers == 0 {
		cfg.Webhook.Workers = 5
	}
	if cfg.Webhook.MaxRetries == 0 {
		cfg.Webhook.MaxRetries = 5
	}
	if cfg.Webhook.BaseBackoff == 0 {
		cfg.Webhook.BaseBackoff = 2
	}
	return &cfg, nil
}
