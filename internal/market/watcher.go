package market

import (
	"math/rand"
	"sync"
	"time"

	"traderider/internal/binance"
)

type MarketWatcher struct {
	mu      sync.RWMutex
	price   float64
	symbol  string
	history []float64
	maxLen  int
	demo    bool
	binance *binance.Client
}

func NewWatcher(demo bool, binClient *binance.Client) *MarketWatcher {
	return &MarketWatcher{
		price:   30000.0,
		symbol:  "BTCUSDC",
		maxLen:  300, // stores ~5 minutes of history at 1-second intervals
		demo:    demo,
		binance: binClient,
	}
}

func (m *MarketWatcher) Start() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()

		if m.demo {
			// Simulate price movement with random fluctuation in demo mode
			m.price += rand.Float64()*200 - 100
		} else {
			if p := m.binance.GetBTCPrice(); p > 0 {
				m.price = p
			}
		}

		// Append new price to history for indicator tracking
		m.history = append(m.history, m.price)
		if len(m.history) > m.maxLen {
			m.history = m.history[1:]
		}

		m.mu.Unlock()
	}
}

func (m *MarketWatcher) GetPrice() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.price
}

func (m *MarketWatcher) GetHistory() []float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Return a copy to avoid exposing internal slice
	return append([]float64{}, m.history...)
}

func (m *MarketWatcher) GetUSDCBalance() (float64, error) {
	if m.binance != nil {
		return m.binance.GetUSDCBalance()
	}
	return 0, nil
}
