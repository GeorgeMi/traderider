package config

import (
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mode string `yaml:"mode"` // "demo" or "real"

	Binance struct {
		APIKey     string `yaml:"api_key"`
		SecretKey  string `yaml:"secret_key"`
		UseTestnet bool   `yaml:"use_testnet"`
	} `yaml:"binance"`

	Strategy struct {
		ShortEMA            int     `yaml:"short_ema"`
		LongEMA             int     `yaml:"long_ema"`
		InvestmentPerTrade  float64 `yaml:"investment_per_trade"`
		MinHoldingThreshold float64 `yaml:"min_holding_threshold"`
		MinHoldMinutes      int     `yaml:"min_hold_minutes"`
		MaxHoldingMinutes   int     `yaml:"max_holding_minutes"`
		MinProfitMargin     float64 `yaml:"min_profit_margin"`
		SoftStopLoss        float64 `yaml:"soft_stop_loss"`
		MinTradeGapPercent  float64 `yaml:"min_trade_gap_percent"`
		UseBollinger        bool    `yaml:"use_bollinger"`
		BollingerWindow     int     `yaml:"bollinger_window"`
		CommissionRate      float64 `yaml:"commission_rate"`
	} `yaml:"strategy"`

	WhatsApp struct {
		Phone  string `yaml:"phone"`
		APIKey string `yaml:"apikey"`
	} `yaml:"whatsapp"`
}

// Load parses a YAML config file from the given path
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
