package config

import (
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mode string `yaml:"mode"` // Supported values: "demo" or "real"

	Binance struct {
		APIKey     string `yaml:"api_key"`
		SecretKey  string `yaml:"secret_key"`
		UseTestnet bool   `yaml:"use_testnet"`
	} `yaml:"binance"`

	Strategy struct {
		InvestmentPerTrade  float64 `yaml:"investment_per_trade"`  // Amount to invest per trade (e.g. 20 USDC)
		MinHoldingThreshold float64 `yaml:"min_holding_threshold"` // Minimum BTC amount to hold (e.g. 0.0002)
		MinHoldMinutes      int     `yaml:"min_hold_minutes"`      // Minimum duration to hold a position (e.g. 2 minutes)
		MaxHoldingMinutes   int     `yaml:"max_holding_minutes"`   // Maximum duration to hold a position (e.g. 120 minutes)
		MinProfitMargin     float64 `yaml:"min_profit_margin"`     // Minimum target profit margin (e.g. 0.005 = 0.5%)
		SoftStopLoss        float64 `yaml:"soft_stop_loss"`        // Maximum tolerated loss before soft exit (e.g. 0.02 = 2%)
		MinTradeGapPercent  float64 `yaml:"min_trade_gap_percent"` // Minimum price gap between sell and next buy
	} `yaml:"strategy"`
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
