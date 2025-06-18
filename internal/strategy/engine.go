package strategy

type StrategyEngine struct {
	ShortWindow     int
	LongWindow      int
	MinProfitMargin float64 // prag minim procentual pentru profit (ex. 0.001 = 0.1%)
	RSIWindow       int     // în general 14
}

func NewEngine(shortWindow, longWindow int, minProfitMargin float64) *StrategyEngine {
	return &StrategyEngine{
		ShortWindow:     shortWindow,
		LongWindow:      longWindow,
		MinProfitMargin: minProfitMargin,
		RSIWindow:       14,
	}
}

func (s *StrategyEngine) ShouldBuy(price float64, history []float64) bool {
	if len(history) < s.LongWindow || len(history) < s.RSIWindow {
		return false
	}

	emaShort := ema(history[len(history)-s.ShortWindow:], s.ShortWindow)
	emaLong := ema(history[len(history)-s.LongWindow:], s.LongWindow)
	rsi := calculateRSI(history, s.RSIWindow)

	// Cumpără dacă trendul e ascendent și RSI indică supravânzare (<30)
	return emaShort > emaLong && price < emaShort && rsi < 30
}

func (s *StrategyEngine) ShouldSell(price, buyPrice float64, history []float64) bool {
	if len(history) < s.LongWindow || len(history) < s.RSIWindow {
		return false
	}

	emaShort := ema(history[len(history)-s.ShortWindow:], s.ShortWindow)
	emaLong := ema(history[len(history)-s.LongWindow:], s.LongWindow)
	rsi := calculateRSI(history, s.RSIWindow)

	// Vinde dacă trendul e descendent și RSI indică supracumpărare (>70)
	return emaShort < emaLong &&
		price > emaShort &&
		price > buyPrice*(1+s.MinProfitMargin) &&
		rsi > 70
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
		return 50 // neutru
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
