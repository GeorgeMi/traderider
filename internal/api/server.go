package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"traderider/internal/market"
	"traderider/internal/trader"
)

type Server struct {
	DB      *sql.DB
	Router  *mux.Router
	Market  *market.MarketWatcher
	Traders map[string]*trader.Trader
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

func NewServer(db *sql.DB, market *market.MarketWatcher, traders map[string]*trader.Trader) *Server {
	s := &Server{
		DB:      db,
		Market:  market,
		Traders: traders,
		Router:  mux.NewRouter(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.Router.HandleFunc("/api/transactions/{symbol}", s.handleTransactions).Methods("GET")
	s.Router.HandleFunc("/api/summary/{symbol}", s.handleSummary).Methods("GET")
	s.Router.HandleFunc("/api/chart-data/{symbol}", s.handleChartData).Methods("GET")
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
