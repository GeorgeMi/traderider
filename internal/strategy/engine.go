package strategy

import (
	"fmt"
	"math"
	"time"
)

type StrategyEngine struct {
	ShortWindow        int
	LongWindow         int
	MinProfitMargin    float64
	RSIWindow          int
	LastSellTime       time.Time
	LastSellProfit     float64
	MinTradeGapPercent float64
	LastBuyTime        time.Time
	SoftStopLoss       float64
	MaxHoldingDuration time.Duration
	LastEMAShort       float64
	LastEMALong        float64
	LastRSI            float64
	CommissionRate     float64
	UseBollinger       bool
	BollingerWindow    int
}

func NewEngine(shortWindow, longWindow int, minProfitMargin float64) *StrategyEngine {
	return &StrategyEngine{
		ShortWindow:        shortWindow,
		LongWindow:         longWindow,
		MinProfitMargin:    minProfitMargin,
		RSIWindow:          14,
		MinTradeGapPercent: 0.003,
		SoftStopLoss:       0.02,
		MaxHoldingDuration: 2 * time.Hour,
		CommissionRate:     0.001,
		UseBollinger:       true,
		BollingerWindow:    20,
	}
}

func (s *StrategyEngine) ShouldBuy(price float64, history []float64, lastSellPrice float64) bool {
	if len(history) < s.LongWindow || len(history) < s.RSIWindow+1 {
		return false
	}

	emaShort := s.CalculateEMAShort(history)
	emaLong := s.CalculateEMALong(history)
	rsi := s.CalculateRSI(history)

	s.LastEMAShort = emaShort
	s.LastEMALong = emaLong
	s.LastRSI = rsi

	trendUp := emaShort > emaLong
	priceAboveEMA := price > emaShort
	rsiMomentum := rsi > 55
	minGapOK := lastSellPrice == 0 || price > lastSellPrice*(1+s.MinTradeGapPercent)
	timeOK := time.Since(s.LastSellTime) > 3*time.Minute || s.LastSellProfit >= 0.5

	bollLower, _, bollUpper := 0.0, 0.0, 0.0
	priceNearLowerBand := true
	if s.UseBollinger {
		bollLower, _, bollUpper = calculateBollingerBands(history, s.BollingerWindow)
		priceNearLowerBand = price < bollLower*1.02
		fmt.Printf("[BOLL] price=%.2f, lower=%.2f, upper=%.2f\n", price, bollLower, bollUpper)
	}

	score := 0
	if trendUp {
		score++
	}
	if priceAboveEMA {
		score++
	}
	if rsiMomentum {
		score++
	}
	if minGapOK {
		score++
	}
	if timeOK {
		score++
	}
	if priceNearLowerBand {
		score++
	}

	fmt.Printf("[STRATEGY] BUY check: price=%.2f, emaShort=%.2f, emaLong=%.2f, rsi=%.2f, score=%d/6\n",
		price, emaShort, emaLong, rsi, score)

	return score >= 5
}

func (s *StrategyEngine) ShouldSell(price, buyPrice float64, history []float64) bool {
	if len(history) < s.LongWindow || len(history) < s.RSIWindow+1 {
		return false
	}

	emaShort := s.CalculateEMAShort(history)
	emaLong := s.CalculateEMALong(history)
	rsi := s.CalculateRSI(history)

	s.LastEMAShort = emaShort
	s.LastEMALong = emaLong
	s.LastRSI = rsi

	fmt.Printf("[STRATEGY] SELL check: price=%.2f, avgBuyPrice=%.2f, emaShort=%.2f, emaLong=%.2f, rsi=%.2f\n",
		price, buyPrice, emaShort, emaLong, rsi)

	adjustedProfit := (price * (1 - s.CommissionRate)) - (buyPrice * (1 + s.CommissionRate))
	profitPct := adjustedProfit / (buyPrice * (1 + s.CommissionRate))
	heldDuration := time.Since(s.LastBuyTime)

	if s.UseBollinger {
		_, _, bollUpper := calculateBollingerBands(history, s.BollingerWindow)
		if price > bollUpper && profitPct > s.MinProfitMargin {
			fmt.Printf("[STRATEGY] Price above Bollinger upper band + profit: SELL\n")
			s.LastSellTime = time.Now()
			s.LastSellProfit = profitPct * 100
			return true
		}
	}

	if profitPct > s.MinProfitMargin && rsi > 60 {
		s.LastSellTime = time.Now()
		s.LastSellProfit = profitPct * 100
		fmt.Printf("[STRATEGY] Profit + RSI confirmed: SELL %.2f%%\n", profitPct*100)
		return true
	}

	if profitPct < -s.SoftStopLoss && heldDuration > time.Hour {
		fmt.Printf("[STRATEGY] STOP-LOSS triggered: %.2f%% loss after 1h\n", profitPct*100)
		s.LastSellTime = time.Now()
		s.LastSellProfit = profitPct * 100
		return true
	}

	if profitPct < 0 && profitPct > -0.005 && heldDuration > 45*time.Minute {
		fmt.Printf("[STRATEGY] Defensive exit (-0.5%%) after 45m: SELL %.2f%%\n", profitPct*100)
		s.LastSellTime = time.Now()
		s.LastSellProfit = profitPct * 100
		return true
	}

	if heldDuration > s.MaxHoldingDuration {
		fmt.Printf("[STRATEGY] Max holding duration exceeded (%.0fm): SELL %.2f%%\n", heldDuration.Minutes(), profitPct*100)
		s.LastSellTime = time.Now()
		s.LastSellProfit = profitPct * 100
		return true
	}

	return false
}

func (s *StrategyEngine) CalculateEMAShort(history []float64) float64 {
	return ema(history[len(history)-s.ShortWindow:], s.ShortWindow)
}

func (s *StrategyEngine) CalculateEMALong(history []float64) float64 {
	return ema(history[len(history)-s.LongWindow:], s.LongWindow)
}

func (s *StrategyEngine) CalculateRSI(history []float64) float64 {
	return calculateRSI(history, s.RSIWindow)
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

func calculateBollingerBands(data []float64, window int) (float64, float64, float64) {
	if len(data) < window {
		return 0, 0, 0
	}
	sum := 0.0
	for _, v := range data[len(data)-window:] {
		sum += v
	}
	mean := sum / float64(window)

	stddevSum := 0.0
	for _, v := range data[len(data)-window:] {
		diff := v - mean
		stddevSum += diff * diff
	}
	stddev := math.Sqrt(stddevSum / float64(window))

	lower := mean - 2*stddev
	upper := mean + 2*stddev
	return lower, mean, upper
}
