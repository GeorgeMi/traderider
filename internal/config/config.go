package config

import (
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Binance struct {
		APIKey     string `yaml:"api_key"`
		SecretKey  string `yaml:"secret_key"`
		UseTestnet bool   `yaml:"use_testnet"`
	} `yaml:"binance"`
	Mode string `yaml:"mode"` // "demo" or "real"
}

func Load(path string) *Config {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("failed to open config file: %v", err)
	}
	defer f.Close()

	var cfg Config
	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&cfg); err != nil {
		log.Fatalf("failed to decode config file: %v", err)
	}

	return &cfg
}
