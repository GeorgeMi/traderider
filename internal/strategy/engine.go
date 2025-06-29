package strategy

import (
	"fmt"
	"math"
	"time"
)

type FeatureVector struct {
	Price       float64
	EMAShort    float64
	EMALong     float64
	RSI         float64
	BandLower   float64
	BandUpper   float64
	BandWidth   float64
	LastSellGap float64
	TimeOK      bool
}

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

	// Așteaptă o scădere reală după vânzare
	if lastSellPrice > 0 && price > lastSellPrice*(1-0.005) {
		fmt.Printf("[STRATEGY] Waiting for price to drop further after last SELL (%.2f > %.2f)\n", price, lastSellPrice*(1-0.005))
		return false
	}

	// Confirmă bottom local (minim + două creșteri)
	if !confirmBottomFormation(history) {
		fmt.Printf("[STRATEGY] No local bottom pattern detected\n")
		return false
	}

	features := s.extractFeatures(price, history, lastSellPrice)
	score := s.buyScore(features)

	return score > 1.8
}

func confirmBottomFormation(history []float64) bool {
	if len(history) < 5 {
		return false
	}
	n := len(history)
	return history[n-3] < history[n-4] &&
		history[n-2] > history[n-3] &&
		history[n-1] > history[n-2]
}

func (s *StrategyEngine) ShouldSell(price, buyPrice float64, history []float64) bool {
	if len(history) < s.LongWindow || len(history) < s.RSIWindow+1 {
		return false
	}

	netProfit := ((price * (1 - s.CommissionRate)) - (buyPrice * (1 + s.CommissionRate))) / buyPrice
	holdingTime := time.Since(s.LastBuyTime)

	if netProfit < -s.SoftStopLoss && holdingTime > time.Hour {
		fmt.Printf("[STRATEGY] Stop-loss triggered: %.2f%%\n", netProfit*100)
		s.LastSellTime = time.Now()
		s.LastSellProfit = netProfit * 100
		return true
	}

	// 💡 Smart condition for max holding: avoid selling too early if trend is good
	if holdingTime > s.MaxHoldingDuration {
		features := s.extractFeatures(price, history, buyPrice)
		if netProfit < 0.01 && features.EMAShort < features.EMALong && features.RSI < 45 {
			fmt.Printf("[STRATEGY] Weak trend + low profit after max hold\n")
			s.LastSellTime = time.Now()
			s.LastSellProfit = netProfit * 100
			return true
		}
	}

	features := s.extractFeatures(price, history, buyPrice)
	score := s.sellScore(features, netProfit)
	if score > 1.2 {
		s.LastSellTime = time.Now()
		s.LastSellProfit = netProfit * 100
		return true
	}

	return false
}

func (s *StrategyEngine) extractFeatures(price float64, history []float64, refPrice float64) FeatureVector {
	emaShort := s.CalculateEMAShort(history)
	emaLong := s.CalculateEMALong(history)
	rsi := s.CalculateRSI(history)

	bandLower, _, bandUpper := 0.0, 0.0, 0.0
	if s.UseBollinger {
		bandLower, _, bandUpper = CalculateBollingerBands(history, s.BollingerWindow)
	}

	gap := 0.0
	if refPrice > 0 {
		gap = (price - refPrice) / refPrice
	}
	return FeatureVector{
		Price:       price,
		EMAShort:    emaShort,
		EMALong:     emaLong,
		RSI:         rsi,
		BandLower:   bandLower,
		BandUpper:   bandUpper,
		BandWidth:   bandUpper - bandLower,
		LastSellGap: gap,
		TimeOK:      time.Since(s.LastSellTime) > 3*time.Minute || s.LastSellProfit >= 0.5,
	}
}

func (s *StrategyEngine) buyScore(f FeatureVector) float64 {
	score := 0.0
	if f.EMAShort > f.EMALong {
		score += 1.0
	}
	if f.Price < f.BandLower*1.02 {
		score += 0.8
	}
	if f.RSI > 50 {
		score += 0.5
	}
	if f.LastSellGap > s.MinTradeGapPercent {
		score += 0.4
	}
	if f.TimeOK {
		score += 0.5
	}
	fmt.Printf("[SCORE][BUY] price=%.2f ema=%.2f/%.2f rsi=%.2f bandL=%.2f score=%.2f\n",
		f.Price, f.EMAShort, f.EMALong, f.RSI, f.BandLower, score)
	return score
}

func (s *StrategyEngine) sellScore(f FeatureVector, profit float64) float64 {
	score := 0.0
	if f.Price > f.BandUpper*0.98 {
		score += 0.8
	}
	if f.RSI > 60 {
		score += 0.5
	}
	if profit > s.MinProfitMargin {
		score += 0.5
	}
	return score
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

func CalculateBollingerBands(data []float64, window int) (float64, float64, float64) {
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
