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
		maxLen:  300, // last 10 minutes
		demo:    demo,
		binance: binClient,
	}
}

func (m *MarketWatcher) Start() {
	for {
		time.Sleep(1 * time.Second) // more responsive updates

		m.mu.Lock()
		if m.demo {
			m.price += rand.Float64()*200 - 100
		} else {
			p := m.binance.GetBTCPrice()
			if p > 0 {
				m.price = p
				// fmt.Printf("[MARKET] Price updated: %.2f\n", p)
			}
		}

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
	return append([]float64{}, m.history...)
}

// Am redenumit și funcția pentru USDC
func (m *MarketWatcher) GetUSDCBalance() (float64, error) {
	if m.binance != nil {
		balance, err := m.binance.GetUSDCBalance()
		if err != nil {
			return 0, err
		}
		return balance, nil
	}

	return 0, nil
}
