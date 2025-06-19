package trader

import (
	"fmt"
	"math"
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
	minHoldDuration time.Duration,
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
		trailingStopPct:     0.02, // mai permisiv
		maxEntries:          3,
		cooldownDuration:    60 * time.Second,
		minHoldingThreshold: minHoldingThreshold,
		minHoldDuration:     minHoldDuration,
	}
}

func (t *Trader) Run() {
	for {
		time.Sleep(5 * time.Second)
		price := t.mw.GetPrice()
		history := t.mw.GetHistory()
		value := t.totalValue(price)

		t.updateBalances()
		t.checkSafety(value)
		t.resetIfInvalid(price)

		if t.inCooldown() {
			continue
		}

		if t.canBuy(price, history) {
			t.tryBuy(price)
		} else if t.canSell(price, history) {
			t.trySell(price)
		}
	}
}

func (t *Trader) updateBalances() {
	if !t.demo {
		if realUSDC, err := t.binClient.GetUSDCBalance(); err == nil {
			t.usdcBalance = realUSDC
		}
		if realBTC, err := t.binClient.GetBTCBalance(); err == nil {
			t.btcHeld = realBTC
		}
	}
}

func (t *Trader) checkSafety(value float64) {
	if value < t.dailyStartValue*0.75 {
		fmt.Printf("[WARNING] Total portfolio value %.2f dropped below 75%% of initial value %.2f\n", value, t.dailyStartValue)
	}
}

func (t *Trader) resetIfInvalid(price float64) {
	if t.holding && t.btcHeld > 0 && t.btcHeld < t.minHoldingThreshold {
		notional := t.btcHeld * price
		if notional >= 5 {
			fmt.Printf("[WARNING] BTC holding is low (%.8f), but value %.2f is acceptable — no reset\n", t.btcHeld, notional)
			return
		}
		fmt.Printf("[RESET] BTC holding too low (%.8f, %.2f USD), resetting state.\n", t.btcHeld, notional)
		t.resetState()
	}
}

func (t *Trader) inCooldown() bool {
	if time.Since(t.lastSellTime) < t.cooldownDuration {
		fmt.Printf("[COOLDOWN] Waiting after last sell: %.0fs remaining\n", (t.cooldownDuration - time.Since(t.lastSellTime)).Seconds())
		return true
	}
	return false
}

func (t *Trader) canBuy(price float64, history []float64) bool {
	buyPrice := t.averageBuyPrice
	return (!t.holding || (t.entries < t.maxEntries && price < buyPrice*0.96)) &&
		t.se.ShouldBuy(price, history, t.lastSellPrice)
}

func (t *Trader) tryBuy(price float64) {
	if t.usdcBalance < t.investmentPerTrade {
		fmt.Printf("[SKIP] Not enough USDC to buy (have %.2f, need %.2f)\n", t.usdcBalance, t.investmentPerTrade)
		return
	}
	amount, err := t.binClient.CalculateBuyQty("BTCUSDC", t.investmentPerTrade)
	if err != nil || amount <= 0 {
		fmt.Printf("[SKIP] Cannot buy: %v\n", err)
		return
	}
	notional := amount * price
	if !t.demo {
		if err := t.binClient.MarketBuy("BTCUSDC", amount); err != nil {
			fmt.Printf("[ERROR] Market Buy failed: %v\n", err)
			return
		}
	}
	t.btcHeld += amount
	t.usdcBalance -= notional
	t.usdcInvested += notional
	t.averageBuyPrice = t.usdcInvested / t.btcHeld
	t.trailingHigh = price
	t.entries++
	t.holding = true

	if t.entries == 1 {
		t.se.LastBuyTime = time.Now()
	}
	if t.btcHeld < t.minHoldingThreshold {
		t.minHoldingThreshold = t.btcHeld * 0.9
		fmt.Printf("[ADJUST] Lowered minHoldingThreshold to %.8f\n", t.minHoldingThreshold)
	}
	t.db.LogTransaction("BTCUSDC", "BUY", amount, price)
	fmt.Printf("[TRADE] Bought BTC at %.2f (%.2f USDC used)\n", price, notional)
}

func (t *Trader) canSell(price float64, history []float64) bool {
	if !t.holding || t.btcHeld <= 0 {
		return false
	}
	value := t.btcHeld * price
	if value < 5 {
		fmt.Printf("[SKIP] BTC value %.2f below minNotional\n", value)
		return false
	}

	buyPrice := t.averageBuyPrice
	heldDuration := time.Since(t.se.LastBuyTime)
	if heldDuration < t.minHoldDuration {
		fmt.Printf("[SKIP] Holding time %.0fs under minHoldDuration (%.0fs)\n", heldDuration.Seconds(), t.minHoldDuration.Seconds())
		return false
	}

	if price > t.trailingHigh {
		t.trailingHigh = price
	}
	trailingStop := t.trailingHigh * (1 - t.trailingStopPct)

	commission := t.se.CommissionRate
	grossProfit := (price - buyPrice) / buyPrice
	netProfit := ((price * (1 - commission)) - (buyPrice * (1 + commission))) / buyPrice

	fmt.Printf("[CHECK] Gross=%.4f%%, Net=%.4f%%, RSI=%.2f, Price=%.2f, TrailingHigh=%.2f, Stop=%.2f\n",
		grossProfit*100, netProfit*100, t.se.LastRSI, price, t.trailingHigh, trailingStop)

	if netProfit < t.se.MinProfitMargin {
		fmt.Printf("[SKIP] Net profit %.4f%% below MinProfitMargin %.4f%%\n", netProfit*100, t.se.MinProfitMargin*100)
		return false
	}

	if t.se.LastRSI > 85 && grossProfit > 0.001 {
		fmt.Printf("[TRIGGER] RSI>85 and gross profit > 0.1%%: SELL now at %.2f\n", price)
		return true
	}

	return price < trailingStop || t.se.ShouldSell(price, buyPrice, history)
}

func (t *Trader) trySell(price float64) {
	sellAmount := roundQuantity(t.btcHeld, 0.000001)
	if sellAmount <= 0 {
		fmt.Println("[SKIP] Sell amount too small.")
		return
	}

	usdcReturn := sellAmount * price
	buyPrice := t.averageBuyPrice
	commission := t.se.CommissionRate

	grossProfit := (price - buyPrice) / buyPrice
	netProfit := ((price * (1 - commission)) - (buyPrice * (1 + commission))) / buyPrice
	holdingDuration := time.Since(t.se.LastBuyTime)

	if !t.demo {
		if err := t.binClient.MarketSell("BTCUSDC", sellAmount); err != nil {
			fmt.Printf("[ERROR] Market Sell failed: %v\n", err)
			return
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

	fmt.Printf("[TRADE] Sold BTC at %.2f\n", price)
	fmt.Printf(" - Amount: %.6f BTC (~%.2f USDC)\n", sellAmount, usdcReturn)
	fmt.Printf(" - Gross profit: %.4f%%\n", grossProfit*100)
	fmt.Printf(" - Net profit (%.3f%% fee): %.4f%%\n", commission*100, netProfit*100)
	fmt.Printf(" - Holding duration: %.0f minutes\n", holdingDuration.Minutes())
}

func (t *Trader) resetState() {
	t.btcHeld = 0
	t.holding = false
	t.averageBuyPrice = 0
	t.trailingHigh = 0
	t.entries = 0
	t.usdcInvested = 0
}

func (t *Trader) totalValue(price float64) float64 {
	return t.usdcBalance + t.btcHeld*price
}

func roundQuantity(quantity float64, step float64) float64 {
	return math.Floor(quantity/step) * step
}

func (t *Trader) Summary(price float64) map[string]float64 {
	unrealized := t.btcHeld * price
	return map[string]float64{
		"btcHeld":           t.btcHeld,
		"usdcInvested":      t.usdcInvested,
		"usdcProfit":        t.usdcProfit,
		"usdcBalance":       t.usdcBalance,
		"unrealized":        unrealized,
		"totalValue":        t.totalValue(price),
		"averageBuyPrice":   t.averageBuyPrice,
		"investedNow":       unrealized,
		"usdcInvestedTotal": t.usdcProfit + unrealized,
	}
}
