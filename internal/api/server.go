package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"traderider/internal/market"
)

type Server struct {
	DB     *sql.DB
	Router *mux.Router
	Market *market.MarketWatcher
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

func NewServer(db *sql.DB, mw *market.MarketWatcher) *Server {
	s := &Server{DB: db, Market: mw, Router: mux.NewRouter()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.Router.HandleFunc("/api/transactions", s.handleTransactions).Methods("GET")
	s.Router.HandleFunc("/api/summary", s.handleSummary).Methods("GET")
	s.Router.HandleFunc("/api/chart-data", s.handleChartData).Methods("GET")
}

func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query("SELECT symbol, side, amount, price, time FROM transactions ORDER BY time DESC LIMIT 50")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	type Tx struct {
		Symbol string  `json:"symbol"`
		Side   string  `json:"side"`
		Amount float64 `json:"amount"`
		Price  float64 `json:"price"`
		Time   string  `json:"time"`
	}
	var txs []Tx
	for rows.Next() {
		var tx Tx
		rows.Scan(&tx.Symbol, &tx.Side, &tx.Amount, &tx.Price, &tx.Time)
		txs = append(txs, tx)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(txs)
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query("SELECT side, amount, price FROM transactions")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	var btcHeld, usdcProfit, totalInvested float64
	for rows.Next() {
		var side string
		var amount, price float64
		rows.Scan(&side, &amount, &price)
		if side == "BUY" {
			btcHeld += amount
			totalInvested += amount * price
		} else if side == "SELL" {
			btcHeld -= amount
			usdcProfit += amount * price
			totalInvested -= amount * price
		}
	}
	currentPrice := s.Market.GetPrice()
	investedNow := btcHeld * currentPrice
	var unrealized float64
	if btcHeld > 0 {
		buyPrice := totalInvested / btcHeld
		unrealized = (currentPrice - buyPrice) * btcHeld
	} else {
		unrealized = 0
	}
	usdcBalance, err := s.Market.GetUSDCBalance()
	if err != nil {
		log.Printf("[WARN] Failed to get USDC balance: %v", err)
		usdcBalance = 0
	}
	summary := map[string]interface{}{
		"btcHeld":           btcHeld,
		"investedNow":       investedNow,
		"usdcProfit":        usdcProfit,
		"usdcBalance":       usdcBalance,
		"unrealized":        unrealized,
		"totalValue":        usdcBalance + investedNow,
		"priceNow":          currentPrice,
		"usdcInvestedTotal": totalInvested,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

func (s *Server) handleChartData(w http.ResponseWriter, r *http.Request) {
	history := s.Market.GetHistory()
	var pricePoints []PricePoint

	now := time.Now()
	for i, price := range history {
		timestamp := now.Add(-time.Duration(len(history)-i) * time.Second).Format(time.RFC3339) // <- ✅ ISO format
		pricePoints = append(pricePoints, PricePoint{
			Time:  timestamp,
			Price: price,
		})
	}

	txRows, err := s.DB.Query("SELECT time, price, side, amount FROM transactions ORDER BY time ASC")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer txRows.Close()

	var txPoints []TransactionPoint
	for txRows.Next() {
		var p TransactionPoint
		if err := txRows.Scan(&p.Time, &p.Price, &p.Side, &p.Amount); err == nil {
			// OPTIONAL: convert SQL time to RFC3339 if it's not already (sau verifică formatul în DB)
			parsedTime, err := time.Parse("2006-01-02 15:04:05", p.Time) // adjust if needed
			if err == nil {
				p.Time = parsedTime.Format(time.RFC3339)
			}
			txPoints = append(txPoints, p)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"prices":       pricePoints,
		"transactions": txPoints,
		"currentPrice": s.Market.GetPrice(),
	})
}
