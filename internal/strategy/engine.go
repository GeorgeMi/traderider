package strategy

import (
	"fmt"
	"time"
)

type StrategyEngine struct {
	ShortWindow        int
	LongWindow         int
	MinProfitMargin    float64 // ex: 0.005 = 0.5%
	RSIWindow          int     // de obicei 14
	LastSellTime       time.Time
	LastSellProfit     float64
	MinTradeGapPercent float64 // ex: 0.003 = 0.3% diferență între trades
	LastBuyTime        time.Time
	SoftStopLoss       float64       // ex: 0.02 = 2% pierdere
	MaxHoldingDuration time.Duration // ex: 2h → se vinde forțat dacă stă prea mult
}

func NewEngine(shortWindow, longWindow int, minProfitMargin float64) *StrategyEngine {
	return &StrategyEngine{
		ShortWindow:        shortWindow,
		LongWindow:         longWindow,
		MinProfitMargin:    minProfitMargin,
		RSIWindow:          14,
		MinTradeGapPercent: 0.003,
		SoftStopLoss:       0.02,
		MaxHoldingDuration: 2 * time.Hour, // ✅ default: 2 ore
	}
}

func (s *StrategyEngine) ShouldBuy(price float64, history []float64, lastSellPrice float64) bool {
	if len(history) < s.LongWindow || len(history) < s.RSIWindow+1 {
		return false
	}

	emaShort := ema(history[len(history)-s.ShortWindow:], s.ShortWindow)
	emaLong := ema(history[len(history)-s.LongWindow:], s.LongWindow)
	rsi := calculateRSI(history, s.RSIWindow)

	fmt.Printf("[STRATEGY] BUY check: price=%.2f, emaShort=%.2f, emaLong=%.2f, rsi=%.2f\n", price, emaShort, emaLong, rsi)

	trendUp := emaShort > emaLong
	priceAboveEMA := price > emaShort
	rsiMomentum := rsi > 62

	minGapOK := lastSellPrice == 0 || price > lastSellPrice*(1+s.MinTradeGapPercent)
	timeOK := time.Since(s.LastSellTime) > 3*time.Minute || s.LastSellProfit >= 0.5

	return trendUp && priceAboveEMA && rsiMomentum && minGapOK && timeOK
}

func (s *StrategyEngine) ShouldSell(price, buyPrice float64, history []float64) bool {
	if len(history) < s.LongWindow || len(history) < s.RSIWindow+1 {
		return false
	}

	emaShort := ema(history[len(history)-s.ShortWindow:], s.ShortWindow)
	emaLong := ema(history[len(history)-s.LongWindow:], s.LongWindow)
	rsi := calculateRSI(history, s.RSIWindow)

	fmt.Printf("[STRATEGY] SELL check: price=%.2f, avgBuyPrice=%.2f, emaShort=%.2f, emaLong=%.2f, rsi=%.2f\n",
		price, buyPrice, emaShort, emaLong, rsi)

	profit := (price - buyPrice) / buyPrice
	heldDuration := time.Since(s.LastBuyTime)

	// 1. Profit și RSI bune → trailing sell
	if profit > s.MinProfitMargin && rsi > 60 {
		s.LastSellTime = time.Now()
		s.LastSellProfit = profit * 100
		fmt.Printf("[STRATEGY] Profit suficient + RSI: SELL %.2f%%\n", profit*100)
		return true
	}

	// 2. Stop-loss logic: pierdere > 2% și a trecut 1h
	if profit < -s.SoftStopLoss && heldDuration > time.Hour {
		fmt.Printf("[STRATEGY] STOP-LOSS activat: %.2f%% pierdere după 1h\n", profit*100)
		s.LastSellTime = time.Now()
		s.LastSellProfit = profit * 100
		return true
	}

	// 3. Pierdere mică acceptată după 45 min (ex: -0.5% max)
	if profit < 0 && profit > -0.005 && heldDuration > 45*time.Minute {
		fmt.Printf("[STRATEGY] Exit defensiv (-0.5%%) după 45m: SELL %.2f%%\n", profit*100)
		s.LastSellTime = time.Now()
		s.LastSellProfit = profit * 100
		return true
	}

	// 4. Timp maxim în poziție — vinde oricum
	if heldDuration > s.MaxHoldingDuration {
		fmt.Printf("[STRATEGY] Timp maxim în poziție depășit (%.0fm): SELL %.2f%%\n", heldDuration.Minutes(), profit*100)
		s.LastSellTime = time.Now()
		s.LastSellProfit = profit * 100
		return true
	}

	return false
}

func ema(data []float64, window int) float64 {
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

func calculateRSI(data []float64, window int) float64 {
	if len(data) < window+1 {
		return 50
	}
	var gains, losses float64
	for i := len(data) - window; i < len(data)-1; i++ {
		change := data[i+1] - data[i]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}

	if gains+losses == 0 {
		return 50
	}

	rs := gains / losses
	return 100 - (100 / (1 + rs))
}
