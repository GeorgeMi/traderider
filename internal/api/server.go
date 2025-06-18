package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"traderider/internal/market"
)

type Server struct {
	DB     *sql.DB
	Router *mux.Router
	Market *market.MarketWatcher
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
			totalInvested -= amount * price // corectăm și investiția
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
		"usdcInvestedTotal": totalInvested, // real, după ce scazi vânzările
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

func (s *Server) handleChartData(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query("SELECT time, price, side FROM transactions ORDER BY time ASC")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type Point struct {
		Time     string  `json:"time"`
		Price    float64 `json:"price"`
		Side     string  `json:"side"`
		EmaShort float64 `json:"emaShort,omitempty"`
		EmaLong  float64 `json:"emaLong,omitempty"`
		RSI      float64 `json:"rsi,omitempty"`
	}

	var raw []Point
	var prices []float64

	for rows.Next() {
		var p Point
		rows.Scan(&p.Time, &p.Price, &p.Side)
		raw = append(raw, p)
		prices = append(prices, p.Price)
	}

	shortWindow := 5
	longWindow := 20
	rsiWindow := 14

	for i := range raw {
		if i+1 >= shortWindow {
			raw[i].EmaShort = calcEMA(prices[:i+1], shortWindow)
		}
		if i+1 >= longWindow {
			raw[i].EmaLong = calcEMA(prices[:i+1], longWindow)
		}
		if i+1 >= rsiWindow+1 {
			raw[i].RSI = calcRSI(prices[i-rsiWindow : i+1])
		}
	}

	// Trimite și prețul curent pentru linia de referință în chart
	currentPrice := s.Market.GetPrice()

	w.Header().Set("Content-Type", "application/json")
	if raw == nil {
		raw = []Point{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"points":       raw,
		"currentPrice": currentPrice,
	})
}

func calcEMA(data []float64, window int) float64 {
	if len(data) == 0 {
		return 0
	}
	k := 2.0 / float64(window+1)
	ema := data[0]
	for i := 1; i < len(data); i++ {
		ema = data[i]*k + ema*(1-k)
	}
	return ema
}

func calcRSI(data []float64) float64 {
	if len(data) < 2 {
		return 50
	}
	var gains, losses float64
	for i := 1; i < len(data); i++ {
		delta := data[i] - data[i-1]
		if delta > 0 {
			gains += delta
		} else {
			losses -= delta
		}
	}
	if gains+losses == 0 {
		return 50
	}
	rs := gains / losses
	return 100 - (100 / (1 + rs))
}
