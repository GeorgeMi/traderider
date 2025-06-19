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

	symbols := []string{"BTCUSDC", "XRPUSDC"}

	// Initialize a shared MarketWatcher for all symbols
	marketWatcher := market.NewWatcher(demo, binClient)
	go marketWatcher.Start(symbols)

	// Initialize one Trader per symbol
	traders := make(map[string]*trader.Trader)

	for _, symbol := range symbols {
		strategyEngine := strategy.NewEngine(5, 20, cfg.Strategy.MinProfitMargin)
		strategyEngine.MaxHoldingDuration = time.Duration(cfg.Strategy.MaxHoldingMinutes) * time.Minute

		tr := trader.NewTrader(
			db,
			marketWatcher,
			strategyEngine,
			demo,
			cfg.Strategy.InvestmentPerTrade*5,
			cfg.Strategy.InvestmentPerTrade,
			binClient,
			cfg.Strategy.MinHoldingThreshold,
			time.Duration(cfg.Strategy.MinHoldMinutes)*time.Minute,
		)

		tr.Symbol = symbol
		traders[symbol] = tr

		go tr.Run()
		log.Printf("[INFO] Started trader for %s", symbol)
	}

	// Start HTTP server
	server := api.NewServer(db.DB, marketWatcher, traders)
	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.Handle("/api/", server.Router)

	log.Println("[INFO] TradeRider server is running at http://localhost:1010")
	log.Fatal(http.ListenAndServe(":1010", nil))
}
