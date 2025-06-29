package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"traderider/internal/notifier"

	"traderider/internal/api"
	"traderider/internal/binance"
	"traderider/internal/config"
	"traderider/internal/market"
	"traderider/internal/store"
	"traderider/internal/strategy"
	"traderider/internal/trader"
	"traderider/internal/wallet"
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

func TotalPortfolioValue(symbols []string, wm *wallet.WalletManager, mw *market.MarketWatcher, binClient *binance.Client) float64 {
	total := wm.Balance()
	for _, symbol := range symbols {
		asset := symbol[:len(symbol)-4]
		balance, err := binClient.GetAssetBalance(asset)
		if err == nil {
			price := mw.GetPrice(symbol)
			total += balance * price
		}
	}
	return total
}

func MonitorPortfolioHardStop(symbols []string, wm *wallet.WalletManager, mw *market.MarketWatcher, binClient *binance.Client, startValue float64, stopChans map[string]chan struct{}) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		value := TotalPortfolioValue(symbols, wm, mw, binClient)
		if value < startValue*0.9 {
			log.Printf("[HARD-STOP] Portfolio value %.2f < 90%% of start %.2f. Stopping all traders.", value, startValue)
			for _, ch := range stopChans {
				close(ch)
			}
			return
		}
	}
}

func main() {
	cfg := config.Load("config/config.yml")
	log.Printf("[INFO] Starting TradeRider in %s mode", cfg.Mode)

	db, err := store.NewStore("traderider.db")
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}

	whNotifier := notifier.NewWhatsAppNotifier(cfg.WhatsApp.Phone, cfg.WhatsApp.APIKey)
	binClient := binance.NewClient(cfg.Binance.APIKey, cfg.Binance.SecretKey, whNotifier)
	demo := cfg.Mode != "real"

	wm := wallet.NewWalletManager(demo, binClient, whNotifier)
	go func() {
		for range time.Tick(5 * time.Second) {
			wm.Update()
		}
	}()

	symbols := []string{"BTCUSDC", "XRPUSDC", "SOLUSDC", "LINKUSDC", "SUIUSDC"}
	marketWatcher := market.NewWatcher(demo, binClient)
	go marketWatcher.Start(symbols)

	traders := make(map[string]*trader.Trader)
	stopChans := make(map[string]chan struct{})

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
		se := strategy.NewEngine(
			cfg.Strategy.ShortEMA,
			cfg.Strategy.LongEMA,
			cfg.Strategy.MinProfitMargin,
		)
		se.MaxHoldingDuration = time.Duration(cfg.Strategy.MaxHoldingMinutes) * time.Minute
		se.MinTradeGapPercent = cfg.Strategy.MinTradeGapPercent
		se.SoftStopLoss = cfg.Strategy.SoftStopLoss
		se.UseBollinger = cfg.Strategy.UseBollinger
		se.BollingerWindow = cfg.Strategy.BollingerWindow
		se.CommissionRate = cfg.Strategy.CommissionRate

		stopCh := make(chan struct{})
		stopChans[symbol] = stopCh

		tr := trader.NewTrader(
			db, marketWatcher, se, demo,
			cfg.Strategy.InvestmentPerTrade,
			binClient,
			cfg.Strategy.MinHoldingThreshold,
			time.Duration(cfg.Strategy.MinHoldMinutes)*time.Minute,
			wm,
			stopCh,
			whNotifier,
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

	startPortfolioValue := TotalPortfolioValue(symbols, wm, marketWatcher, binClient)
	go MonitorPortfolioHardStop(symbols, wm, marketWatcher, binClient, startPortfolioValue, stopChans)

	server := api.NewServer(db.DB, marketWatcher, traders, wm, binClient, symbols)
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			log.Println("[REBALANCE] Triggered automatic rebalance")
			server.RebalanceAllocations()
		}
	}()

	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.Handle("/api/", server.Router)

	log.Println("[INFO] TradeRider server running at http://localhost:1010")
	log.Fatal(http.ListenAndServe(":1010", nil))
}
