package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Define the User structure exactly ONCE right here:
type UserConfig struct {
	Username     string   `yaml:"username" json:"username"`
	PasswordHash string   `yaml:"password_hash" json:"-"`
	PublicKeys   []string `yaml:"public_keys" json:"public_keys"`
}

type Config struct {
	Server struct {
		Port          int    `yaml:"port"`
		HostKeyPath   string `yaml:"host_key_path"`
		TrustedCAPath string `yaml:"trusted_ca_path"`
		SessionSecret string `yaml:"session_secret"`
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
		URL       string `yaml:"url"`
		QueueName string `yaml:"queue_name"`
		Enabled   bool   `yaml:"enabled"`
	} `yaml:"rabbitmq"`

	Webhook struct {
		URL         string `yaml:"url"`
		Secret      string `yaml:"secret"`
		Workers     int    `yaml:"workers"`
		MaxRetries  int    `yaml:"max_retries"`
		BaseBackoff int    `yaml:"base_backoff"`
	} `yaml:"webhook"`

	// Use the named struct here! Clean and simple.
	Users []UserConfig `yaml:"users" json:"users"`
}

func LoadConfig(path string) (*Config, error) {
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