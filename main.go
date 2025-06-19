package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"traderider/internal/api"
	"traderider/internal/binance"
	"traderider/internal/config"
	"traderider/internal/market"
	"traderider/internal/store"
	"traderider/internal/strategy"
	"traderider/internal/trader"
)

func loadState(path string) (map[string]trader.StateSnapshot, error) {
	states := make(map[string]trader.StateSnapshot)
	data, err := os.ReadFile(path)
	if err != nil {
		return states, err
	}
	err = json.Unmarshal(data, &states)
	return states, err
}

func saveState(path string, states map[string]trader.StateSnapshot) error {
	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func main() {
	cfg := config.Load("config/config.yml")
	log.Printf("[INFO] Starting TradeRider in %s mode", cfg.Mode)

	db, err := store.NewStore("traderider.db")
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}

	binClient := binance.NewClient(cfg.Binance.APIKey, cfg.Binance.SecretKey)
	demo := cfg.Mode != "real"
	symbols := []string{"BTCUSDC", "XRPUSDC", "SOLUSDC", "LINKUSDC"}

	marketWatcher := market.NewWatcher(demo, binClient)
	go marketWatcher.Start(symbols)

	traders := make(map[string]*trader.Trader)
	stateFile := filepath.Join("data", "state.json")
	os.MkdirAll("data", os.ModePerm)
	loadedStates, _ := loadState(stateFile)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			allStates := make(map[string]trader.StateSnapshot)
			for symbol, tr := range traders {
				allStates[symbol] = tr.SnapshotState()
			}
			saveState(stateFile, allStates)
		}
	}()

	for _, symbol := range symbols {
		se := strategy.NewEngine(5, 20, cfg.Strategy.MinProfitMargin)
		se.MaxHoldingDuration = time.Duration(cfg.Strategy.MaxHoldingMinutes) * time.Minute
		tr := trader.NewTrader(
			db, marketWatcher, se, demo,
			cfg.Strategy.InvestmentPerTrade*5,
			cfg.Strategy.InvestmentPerTrade,
			binClient,
			cfg.Strategy.MinHoldingThreshold,
			time.Duration(cfg.Strategy.MinHoldMinutes)*time.Minute,
		)
		tr.Symbol = symbol

		if state, ok := loadedStates[symbol]; ok {
			tr.RestoreState(state)
			log.Printf("[RESTORE] %s: Holding=%.6f AvgBuy=%.2f Entries=%d", symbol, state.AssetHeld, state.AverageBuyPrice, state.Entries)
		}

		traders[symbol] = tr
		go tr.Run()
		log.Printf("[INFO] Started trader for %s", symbol)
	}

	server := api.NewServer(db.DB, marketWatcher, traders)
	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.Handle("/api/", server.Router)
	log.Println("[INFO] TradeRider server is running at http://localhost:1010")
	log.Fatal(http.ListenAndServe(":1010", nil))
}
