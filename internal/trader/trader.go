package trader

import (
	"fmt"
	"time"

	"traderider/internal/binance"
	"traderider/internal/market"
	"traderider/internal/store"
	"traderider/internal/strategy"
)

type Trader struct {
	db                  *store.Store
	mw                  *market.MarketWatcher
	se                  *strategy.StrategyEngine
	binClient           *binance.Client
	demo                bool
	usdcBalance         float64
	btcHeld             float64
	usdcInvested        float64
	usdcProfit          float64
	dailyStartValue     float64
	investmentPerTrade  float64
	holding             bool
	averageBuyPrice     float64
	trailingHigh        float64
	trailingStopPct     float64
	entries             int
	maxEntries          int
	lastSellTime        time.Time
	cooldownDuration    time.Duration
	lastSellPrice       float64
	minHoldingThreshold float64
	minHoldDuration     time.Duration
}

func NewTrader(
	db *store.Store,
	mw *market.MarketWatcher,
	se *strategy.StrategyEngine,
	demo bool,
	usdcBalance float64,
	investmentPerTrade float64,
	binClient *binance.Client,
	minHoldingThreshold float64,
	minHoldDuration time.Duration, // ✅ NOU
) *Trader {
	return &Trader{
		db:                  db,
		mw:                  mw,
		se:                  se,
		binClient:           binClient,
		demo:                demo,
		usdcBalance:         usdcBalance,
		dailyStartValue:     usdcBalance,
		investmentPerTrade:  investmentPerTrade,
		holding:             false,
		trailingStopPct:     0.015,
		maxEntries:          3,
		cooldownDuration:    60 * time.Second,
		minHoldingThreshold: minHoldingThreshold,
		minHoldDuration:     minHoldDuration, // ✅
	}
}

func roundQuantity(quantity float64) float64 {
	step := 0.00000001
	return float64(int(quantity/step)) * step
}

func (t *Trader) Run() {
	for {
		time.Sleep(5 * time.Second)

		price := t.mw.GetPrice()
		history := t.mw.GetHistory()

		value := t.totalValue(price)
		if value < t.dailyStartValue*0.75 {
			fmt.Printf("[WARNING] Total value %.2f dropped below 75%% of start value %.2f.\n", value, t.dailyStartValue)
		}

		if !t.demo {
			if realUSDC, err := t.binClient.GetUSDCBalance(); err == nil {
				t.usdcBalance = realUSDC
			}
			if realBTC, err := t.binClient.GetBTCBalance(); err == nil {
				t.btcHeld = realBTC
			}
		}

		buyPrice := t.averageBuyPrice

		if t.holding && t.btcHeld < t.minHoldingThreshold {
			fmt.Printf("[RESET] Holding BTC too low (%.8f), resetting state.\n", t.btcHeld)
			t.btcHeld = 0
			t.holding = false
			t.averageBuyPrice = 0
			t.trailingHigh = 0
			t.entries = 0
			t.usdcInvested = 0
		}

		if time.Since(t.lastSellTime) < t.cooldownDuration {
			fmt.Printf("[COOLDOWN] Waiting after last sell. %.0fs remaining\n", (t.cooldownDuration - time.Since(t.lastSellTime)).Seconds())
			continue
		}

		// BUY
		if !t.holding || (t.entries < t.maxEntries && price < t.averageBuyPrice*0.96) {
			if t.se.ShouldBuy(price, history, t.lastSellPrice) {
				if t.usdcBalance < t.investmentPerTrade {
					fmt.Println("[SKIP] Not enough USDC balance to buy")
					continue
				}

				amount := roundQuantity(t.investmentPerTrade / price)
				realInvested := amount * price

				if !t.demo {
					if err := t.binClient.MarketBuy("BTCUSDC", amount); err != nil {
						fmt.Printf("[ERROR] Market Buy failed: %v\n", err)
						continue
					}
				}

				t.btcHeld += amount
				t.usdcBalance -= realInvested
				t.usdcInvested += realInvested
				t.averageBuyPrice = t.usdcInvested / t.btcHeld
				t.trailingHigh = price
				t.entries++
				t.holding = true

				if t.entries == 1 {
					t.se.LastBuyTime = time.Now()
				}

				t.db.LogTransaction("BTCUSDC", "BUY", amount, price)
				fmt.Printf("[TRADE] Buying BTC at %.2f (%.2f USDC real)\n", price, realInvested)
			}
		} else if t.holding {
			// SELL
			if t.entries == 1 || price > t.trailingHigh {
				t.trailingHigh = price
			}

			trailingStop := t.trailingHigh * (1 - t.trailingStopPct)
			heldDuration := time.Since(t.se.LastBuyTime)

			shouldSell := ((price < trailingStop && price > buyPrice*(1+t.se.MinProfitMargin)) ||
				t.se.ShouldSell(price, buyPrice, history)) && heldDuration > t.minHoldDuration

			fmt.Printf("[CHECK] trailingHigh=%.2f, trailingStop=%.2f, price=%.2f\n", t.trailingHigh, trailingStop, price)

			if shouldSell {
				sellAmount := roundQuantity(t.btcHeld)
				if sellAmount <= 0 {
					continue
				}

				usdcReturn := sellAmount * price
				if !t.demo {
					if err := t.binClient.MarketSell("BTCUSDC", sellAmount); err != nil {
						fmt.Printf("[ERROR] Market Sell failed: %v\n", err)
						continue
					}
				}

				t.btcHeld = 0
				t.holding = false
				t.usdcBalance += usdcReturn
				t.usdcProfit += usdcReturn - t.usdcInvested
				t.usdcInvested = 0
				t.averageBuyPrice = 0
				t.trailingHigh = 0
				t.entries = 0
				t.lastSellTime = time.Now()
				t.lastSellPrice = price
				t.db.LogTransaction("BTCUSDC", "SELL", sellAmount, price)

				fmt.Printf("[TRADE] Selling BTC at %.2f (%.2f USDC real) after holding for %.0f minutes\n", price, usdcReturn, heldDuration.Minutes())
			}
		}
	}
}

func (t *Trader) totalValue(price float64) float64 {
	return t.usdcBalance + t.btcHeld*price
}

func (t *Trader) Summary(price float64) map[string]float64 {
	unrealized := t.btcHeld * price
	return map[string]float64{
		"btcHeld":         t.btcHeld,
		"usdcInvested":    t.usdcInvested,
		"usdcProfit":      t.usdcProfit,
		"usdcBalance":     t.usdcBalance,
		"unrealized":      unrealized,
		"totalValue":      t.totalValue(price),
		"averageBuyPrice": t.averageBuyPrice,
	}
}
