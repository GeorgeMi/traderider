package main

import (
	"log"
	"net/http"
	"time"

	"traderider/internal/api"
	"traderider/internal/binance"
	"traderider/internal/config"
	"traderider/internal/market"
	"traderider/internal/store"
	"traderider/internal/strategy"
	"traderider/internal/trader"
)

func main() {
	// Load application configuration
	cfg := config.Load("config/config.yml")
	log.Printf("[INFO] Starting TradeRider in %s mode", cfg.Mode)

	// Initialize local store (SQLite)
	db, err := store.NewStore("traderider.db")
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}

	// Initialize Binance client
	binClient := binance.NewClient(cfg.Binance.APIKey, cfg.Binance.SecretKey)
	demo := cfg.Mode != "real"

	// Retrieve initial USDC balance (real or fallback)
	usdcBalance := 1000.0
	if !demo {
		if realBalance, err := binClient.GetUSDCBalance(); err == nil {
			usdcBalance = realBalance
		} else {
			log.Printf("[WARN] Could not fetch USDC balance, using default: %v", err)
		}
	}

	// Start market data watcher
	mw := market.NewWatcher(demo, binClient)
	go mw.Start()

	// Configure strategy engine (e.g. EMA 5/20 with 0.5% profit margin)
	se := strategy.NewEngine(5, 20, 0.005)
	if cfg.Strategy.MaxHoldingMinutes > 0 {
		se.MaxHoldingDuration = time.Duration(cfg.Strategy.MaxHoldingMinutes) * time.Minute
		log.Printf("[INFO] Max holding duration set to %d minutes", cfg.Strategy.MaxHoldingMinutes)
	}

	// Create and launch trader
	t := trader.NewTrader(
		db,
		mw,
		se,
		demo,
		usdcBalance,
		cfg.Strategy.InvestmentPerTrade,
		binClient,
		cfg.Strategy.MinHoldingThreshold,
		time.Duration(cfg.Strategy.MinHoldMinutes)*time.Minute,
	)
	go t.Run()

	// Start API server
	srv := api.NewServer(db.DB, mw, t)

	// Setup HTTP routes
	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.Handle("/api/", srv.Router)

	log.Println("[INFO] TradeRider server running at http://localhost:1010")
	log.Fatal(http.ListenAndServe(":1010", nil))
}
