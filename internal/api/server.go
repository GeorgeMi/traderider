package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"traderider/internal/binance"
	"traderider/internal/market"
	"traderider/internal/trader"
	"traderider/internal/wallet"
)

type Server struct {
	DB           *sql.DB
	Router       *mux.Router
	Market       *market.MarketWatcher
	Traders      map[string]*trader.Trader
	Wallet       *wallet.WalletManager
	Binance      *binance.Client
	TrackedPairs []string
}

type PricePoint struct {
	Time  string  `json:"time"`
	Price float64 `json:"price"`
}

type TransactionPoint struct {
	Time   string  `json:"time"`
	Price  float64 `json:"price"`
	Side   string  `json:"side"`
	Amount float64 `json:"amount"`
}

func NewServer(db *sql.DB, market *market.MarketWatcher, traders map[string]*trader.Trader, wallet *wallet.WalletManager, binClient *binance.Client, symbols []string) *Server {
	s := &Server{
		DB:           db,
		Market:       market,
		Traders:      traders,
		Wallet:       wallet,
		Binance:      binClient,
		TrackedPairs: symbols,
		Router:       mux.NewRouter(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.Router.HandleFunc("/api/transactions/{symbol}", s.handleTransactions).Methods("GET")
	s.Router.HandleFunc("/api/summary/{symbol}", s.handleSummary).Methods("GET")
	s.Router.HandleFunc("/api/chart-data/{symbol}", s.handleChartData).Methods("GET")
	s.Router.HandleFunc("/api/wallet", s.handleWallet).Methods("GET")
	s.Router.HandleFunc("/api/force-sell/{symbol}", s.handleForceSell).Methods("POST")
	s.Router.HandleFunc("/api/performance", s.handlePerformance).Methods("GET")
	s.Router.HandleFunc("/api/rebalance", s.handleRebalance).Methods("GET")
}

func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	symbol := mux.Vars(r)["symbol"]

	rows, err := s.DB.Query("SELECT symbol, side, amount, price, time FROM transactions WHERE symbol = ? ORDER BY time DESC LIMIT 50", symbol)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var txs []map[string]interface{}
	for rows.Next() {
		var symbol, side, timeStr string
		var amount, price float64
		if err := rows.Scan(&symbol, &side, &amount, &price, &timeStr); err == nil {
			txs = append(txs, map[string]interface{}{
				"symbol": symbol,
				"side":   side,
				"amount": amount,
				"price":  price,
				"time":   timeStr,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(txs)
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	symbol := mux.Vars(r)["symbol"]
	tr, ok := s.Traders[symbol]
	if !ok {
		http.Error(w, "Unknown symbol", http.StatusBadRequest)
		return
	}

	price := s.Market.GetPrice(symbol)
	rawSummary := tr.Summary(price)

	summary := make(map[string]interface{})
	for k, v := range rawSummary {
		summary[k] = v
	}

	assetHeld := rawSummary["assetHeld"]
	usdcInvested := rawSummary["usdcInvested"]
	usdcProfit := rawSummary["usdcProfit"]

	summary["priceNow"] = price
	summary["investedNow"] = assetHeld * price
	summary["usdcInvestedTotal"] = usdcInvested + usdcProfit

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

func (s *Server) handleChartData(w http.ResponseWriter, r *http.Request) {
	symbol := mux.Vars(r)["symbol"]
	history := s.Market.GetHistory(symbol)

	var pricePoints []PricePoint
	startTime := time.Now().Add(-time.Duration(len(history)) * time.Second)

	for i, price := range history {
		timestamp := startTime.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
		pricePoints = append(pricePoints, PricePoint{
			Time:  timestamp,
			Price: price,
		})
	}

	txRows, err := s.DB.Query("SELECT time, price, side, amount FROM transactions WHERE symbol = ? ORDER BY time ASC", symbol)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer txRows.Close()

	var txPoints []TransactionPoint
	for txRows.Next() {
		var p TransactionPoint
		var rawTime string
		if err := txRows.Scan(&rawTime, &p.Price, &p.Side, &p.Amount); err == nil {
			if t, err := time.Parse("2006-01-02 15:04:05", rawTime); err == nil {
				p.Time = t.Format(time.RFC3339)
			} else {
				p.Time = rawTime
			}
			txPoints = append(txPoints, p)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"prices":       pricePoints,
		"transactions": txPoints,
		"currentPrice": s.Market.GetPrice(symbol),
	})
}

// 🔹 TotalWallet handler
func (s *Server) handleWallet(w http.ResponseWriter, r *http.Request) {
	total := TotalPortfolioValue(s.TrackedPairs, s.Wallet, s.Market, s.Binance)
	resp := map[string]float64{
		"totalWalletValue": total,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// 🔹 Util func
func TotalPortfolioValue(symbols []string, wm *wallet.WalletManager, mw *market.MarketWatcher, binClient *binance.Client) float64 {
	total := wm.Balance()
	for _, symbol := range symbols {
		asset := symbol[:len(symbol)-4] // ex: BTCUSDC → BTC
		balance, err := binClient.GetAssetBalance(asset)
		if err == nil {
			price := mw.GetPrice(symbol)
			total += balance * price
		}
	}
	return total
}

func (s *Server) handleForceSell(w http.ResponseWriter, r *http.Request) {
	symbol := mux.Vars(r)["symbol"]
	tr, ok := s.Traders[symbol]
	if !ok {
		http.Error(w, "Trader not found", http.StatusNotFound)
		return
	}

	tr.ForceSell()
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Force sell executed"))
}

func (s *Server) handleRebalance(w http.ResponseWriter, r *http.Request) {
	s.RebalanceAllocations()
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Rebalanced"))
}

func (s *Server) RebalanceAllocations() {
	scores := make(map[string]float64)
	totalScore := 0.0

	for _, symbol := range s.TrackedPairs {
		rows, err := s.DB.Query(`
			SELECT side, amount, price, time FROM transactions
			WHERE symbol = ? ORDER BY time ASC
		`, symbol)
		if err != nil {
			continue
		}
		defer rows.Close()

		var stats PerformanceStats
		var buyStack []struct {
			amount float64
			price  float64
			time   time.Time
		}

		commission := 0.001
		var holdingDurations []float64

		for rows.Next() {
			var side string
			var price, amt float64
			var ts string
			_ = rows.Scan(&side, &amt, &price, &ts)
			timeParsed, _ := time.Parse("2006-01-02 15:04:05", ts)

			if side == "BUY" {
				buyStack = append(buyStack, struct {
					amount float64
					price  float64
					time   time.Time
				}{amt, price, timeParsed})
			} else if side == "SELL" && len(buyStack) > 0 {
				amtLeft := amt
				for len(buyStack) > 0 && amtLeft > 0 {
					buy := buyStack[0]
					qty := math.Min(amtLeft, buy.amount)

					buyPrice := buy.price * (1 + commission)
					sellPrice := price * (1 - commission)
					profit := (sellPrice - buyPrice) * qty

					stats.TotalProfit += profit
					stats.TotalTrades++
					if profit > 0 {
						stats.Wins++
						stats.AvgProfit += profit
					} else {
						stats.Losses++
						stats.AvgLoss += profit
					}

					holdingTime := timeParsed.Sub(buy.time).Minutes()
					holdingDurations = append(holdingDurations, holdingTime)

					amtLeft -= qty
					if qty < buy.amount {
						buyStack[0].amount -= qty
						break
					} else {
						buyStack = buyStack[1:]
					}
				}
			}
		}

		if stats.Wins > 0 {
			stats.AvgProfit /= float64(stats.Wins)
		}
		if stats.Losses > 0 {
			stats.AvgLoss /= float64(stats.Losses)
		}
		if stats.TotalTrades > 0 {
			stats.WinRate = float64(stats.Wins) / float64(stats.TotalTrades)
		}
		var avgHold float64
		for _, h := range holdingDurations {
			avgHold += h
		}
		if len(holdingDurations) > 0 {
			avgHold /= float64(len(holdingDurations))
		}

		// Compute normalized score
		lossAbs := math.Abs(stats.AvgLoss) + 0.01
		confidence := math.Log(float64(stats.TotalTrades) + 1)
		score := (stats.TotalProfit / lossAbs) * stats.WinRate * confidence / (avgHold + 1)
		score = math.Max(score, 0.01) // prevent zero weight
		scores[symbol] = score
		totalScore += score
	}

	totalAvailable := 0.8 * s.Wallet.Balance()
	for symbol, score := range scores {
		weight := score / totalScore
		amount := weight * totalAvailable
		s.Traders[symbol].SetInvestmentPerTrade(amount)
		log.Printf("[REBALANCE] %s → %.2f USDC (%.1f%%)\n", symbol, amount, weight*100)
	}
}
