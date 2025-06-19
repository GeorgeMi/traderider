package market

import (
	"math/rand"
	"sync"
	"time"

	"traderider/internal/binance"
)

type MarketWatcher struct {
	mu      sync.RWMutex
	prices  map[string]float64
	history map[string][]float64
	maxLen  int
	demo    bool
	binance *binance.Client
}

// NewWatcher creates a MarketWatcher that tracks multiple symbols.
func NewWatcher(demo bool, binClient *binance.Client) *MarketWatcher {
	return &MarketWatcher{
		prices:  make(map[string]float64),
		history: make(map[string][]float64),
		maxLen:  300, // ~5 minutes of data at 1s intervals
		demo:    demo,
		binance: binClient,
	}
}

// Start begins polling prices for the given symbols in a goroutine.
func (m *MarketWatcher) Start(symbols []string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		for _, symbol := range symbols {
			price := m.fetchPrice(symbol)
			m.prices[symbol] = price
			m.history[symbol] = append(m.history[symbol], price)

			if len(m.history[symbol]) > m.maxLen {
				m.history[symbol] = m.history[symbol][1:]
			}
		}
		m.mu.Unlock()
	}
}

// fetchPrice retrieves the price for a symbol from Binance or simulates it in demo mode.
func (m *MarketWatcher) fetchPrice(symbol string) float64 {
	if m.demo {
		base := m.prices[symbol]
		if base == 0 {
			base = 100.0
		}
		return base + rand.Float64()*2 - 1
	}
	return m.binance.GetSymbolPrice(symbol)
}

// GetPrice returns the latest price for a symbol.
func (m *MarketWatcher) GetPrice(symbol string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.prices[symbol]
}

// GetHistory returns the price history for a symbol.
func (m *MarketWatcher) GetHistory(symbol string) []float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]float64{}, m.history[symbol]...)
}

// GetUSDCBalance returns the current USDC balance from Binance.
func (m *MarketWatcher) GetUSDCBalance() (float64, error) {
	if m.binance != nil {
		return m.binance.GetUSDCBalance()
	}
	return 0, nil
}
