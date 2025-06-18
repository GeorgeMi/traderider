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
	db                 *store.Store
	mw                 *market.MarketWatcher
	se                 *strategy.StrategyEngine
	binClient          *binance.Client
	demo               bool
	usdcBalance        float64
	btcHeld            float64
	usdcInvested       float64
	usdcProfit         float64
	dailyStartValue    float64
	investmentPerTrade float64
	holding            bool
	lastBuyPrice       float64
}

func NewTrader(db *store.Store, mw *market.MarketWatcher, se *strategy.StrategyEngine, demo bool, usdcBalance float64, investmentPerTrade float64, binClient *binance.Client) *Trader {
	return &Trader{
		db:                 db,
		mw:                 mw,
		se:                 se,
		binClient:          binClient,
		demo:               demo,
		usdcBalance:        usdcBalance,
		dailyStartValue:    usdcBalance,
		investmentPerTrade: investmentPerTrade,
		holding:            false,
		lastBuyPrice:       0,
	}
}

func roundQuantity(quantity float64) float64 {
	step := 0.0001
	return float64(int(quantity/step)) * step
}

func (t *Trader) Run() {
	for {
		time.Sleep(5 * time.Second)

		price := t.mw.GetPrice()
		history := t.mw.GetHistory()

		value := t.totalValue(price)
		threshold := t.dailyStartValue * 0.75

		if value < threshold {
			fmt.Printf("[STOP] Total value %.2f dropped below 75%% of start value %.2f. Stopping.\n", value, t.dailyStartValue)
			return
		}

		if !t.demo {
			if realUSDC, err := t.binClient.GetUSDCBalance(); err == nil {
				t.usdcBalance = realUSDC
			}
			if realBTC, err := t.binClient.GetBTCBalance(); err == nil {
				t.btcHeld = realBTC
			}
		}

		buyPrice := t.lastBuyPrice

		if !t.holding {
			if t.se.ShouldBuy(price, history) {
				investAmount := t.investmentPerTrade

				if t.usdcBalance < investAmount {
					if t.btcHeld > 0 {
						fmt.Println("[INFO] Not enough USDC, selling partial BTC to generate USDC")
						partialSellAmount := roundQuantity(t.btcHeld * 0.5)
						err := t.binClient.MarketSell("BTCUSDC", partialSellAmount)
						if err == nil {
							t.btcHeld -= partialSellAmount
							t.usdcBalance += partialSellAmount * price
							t.db.LogTransaction("BTCUSDC", "SELL", partialSellAmount, price)
						}
					}
					fmt.Println("[SKIP] Not enough USDC balance to buy")
					continue
				}

				amount := roundQuantity(investAmount / price)

				if !t.demo {
					err := t.binClient.MarketBuy("BTCUSDC", amount)
					if err != nil {
						fmt.Printf("[ERROR] Market Buy failed: %v\n", err)
						continue
					}
				}

				t.btcHeld += amount
				t.usdcBalance -= investAmount
				t.usdcInvested += investAmount
				t.lastBuyPrice = price
				t.db.LogTransaction("BTCUSDC", "BUY", amount, price)
				t.holding = true
				fmt.Printf("[TRADE] Buying BTC at %.2f (%.2f USDC)\n", price, investAmount)
			} else {
				fmt.Printf("[DEBUG] No BUY: price=%.2f, holding=%.4f\n", price, t.btcHeld)
			}
		} else {
			if t.se.ShouldSell(price, buyPrice, history) {
				sellAmount := roundQuantity(t.btcHeld)
				if sellAmount <= 0 {
					continue
				}

				if !t.demo {
					err := t.binClient.MarketSell("BTCUSDC", sellAmount)
					if err != nil {
						fmt.Printf("[ERROR] Market Sell failed: %v\n", err)
						continue
					}
				}

				t.btcHeld -= sellAmount
				usdcReturn := sellAmount * price
				t.usdcBalance += usdcReturn
				t.usdcProfit += usdcReturn - t.usdcInvested
				t.db.LogTransaction("BTCUSDC", "SELL", sellAmount, price)
				fmt.Printf("[TRADE] Selling BTC at %.2f (%.2f USDC)\n", price, usdcReturn)

				if t.btcHeld <= 0 {
					t.btcHeld = 0
					t.usdcInvested = 0
					t.lastBuyPrice = 0
					t.holding = false
				}
			} else {
				fmt.Printf("[DEBUG] No SELL: price=%.2f, buyPrice=%.2f, holding=%.4f\n", price, buyPrice, t.btcHeld)
			}
		}
	}
}

func (t *Trader) totalValue(price float64) float64 {
	return t.usdcBalance + t.btcHeld*price
}

func (t *Trader) Summary(price float64) map[string]float64 {
	unrealized := t.btcHeld * price
	total := t.totalValue(price)
	return map[string]float64{
		"btcHeld":      t.btcHeld,
		"usdcInvested": t.usdcInvested,
		"usdcProfit":   t.usdcProfit,
		"usdcBalance":  t.usdcBalance,
		"unrealized":   unrealized,
		"totalValue":   total,
	}
}
