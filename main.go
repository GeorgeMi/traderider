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
	cfg := config.Load("config/config.yml")

	log.Printf("[INFO] Starting TradeRider in %s mode", cfg.Mode)

	db, err := store.NewStore("traderider.db")
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}

	binClient := binance.NewClient(cfg.Binance.APIKey, cfg.Binance.SecretKey)
	demo := cfg.Mode != "real"

	usdcBalance := 1000.0
	realBalance, err := binClient.GetUSDCBalance()
	if err != nil {
		log.Printf("[WARN] Failed to fetch real USDC balance, using default: %v", err)
	} else {
		usdcBalance = realBalance
	}

	mw := market.NewWatcher(demo, binClient)
	go mw.Start()

	se := strategy.NewEngine(5, 20, 0.001)
	if cfg.Strategy.MaxHoldingMinutes > 0 {
		se.MaxHoldingDuration = time.Duration(cfg.Strategy.MaxHoldingMinutes) * time.Minute
		log.Printf("[INFO] Max holding duration set to %d minutes", cfg.Strategy.MaxHoldingMinutes)
	}

	t := trader.NewTrader(
		db,
		mw,
		se,
		demo,
		usdcBalance,
		30.0,
		binClient,
		cfg.Strategy.MinHoldingThreshold,
		2*time.Minute,
	)
	go t.Run()

	srv := api.NewServer(db.DB, mw)

	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.Handle("/api/", srv.Router)

	log.Println("[INFO] TradeRider server running at http://localhost:1010")
	log.Fatal(http.ListenAndServe(":1010", nil))
}
