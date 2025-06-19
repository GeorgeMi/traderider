package trader

import (
	"fmt"
	"math"
	"strings"
	"time"

	"traderider/internal/binance"
	"traderider/internal/market"
	"traderider/internal/store"
	"traderider/internal/strategy"
)

type Trader struct {
	Symbol              string
	db                  *store.Store
	mw                  *market.MarketWatcher
	se                  *strategy.StrategyEngine
	binClient           *binance.Client
	demo                bool
	usdcBalance         float64
	assetHeld           float64
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
		trailingStopPct:     0.02,
		maxEntries:          3,
		cooldownDuration:    60 * time.Second,
		minHoldingThreshold: minHoldingThreshold,
		minHoldDuration:     minHoldDuration,
	}
}

func (t *Trader) Run() {
	for {
		time.Sleep(5 * time.Second)
		price := t.mw.GetPrice(t.Symbol)
		history := t.mw.GetHistory(t.Symbol)
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
		asset := strings.Replace(t.Symbol, "USDC", "", 1)
		if realAsset, err := t.binClient.GetAssetBalance(asset); err == nil {
			t.assetHeld = realAsset
		}
	}
}

func (t *Trader) checkSafety(value float64) {
	if value < t.dailyStartValue*0.75 {
		fmt.Printf("[WARNING] [%s] Total value %.2f dropped below 75%% of initial %.2f\n", t.Symbol, value, t.dailyStartValue)
	}
}

func (t *Trader) resetIfInvalid(price float64) {
	if t.holding && t.assetHeld > 0 && t.assetHeld < t.minHoldingThreshold {
		notional := t.assetHeld * price
		if notional >= 5 {
			fmt.Printf("[INFO] [%s] Low holding (%.8f), but value %.2f is acceptable\n", t.Symbol, t.assetHeld, notional)
			return
		}
		fmt.Printf("[RESET] [%s] Holding too low (%.8f, %.2f USD), resetting\n", t.Symbol, t.assetHeld, notional)
		t.resetState()
	}
}

func (t *Trader) inCooldown() bool {
	if time.Since(t.lastSellTime) < t.cooldownDuration {
		fmt.Printf("[COOLDOWN] [%s] %.0fs remaining\n", t.Symbol, (t.cooldownDuration - time.Since(t.lastSellTime)).Seconds())
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
		fmt.Printf("[SKIP] [%s] Not enough USDC (%.2f < %.2f)\n", t.Symbol, t.usdcBalance, t.investmentPerTrade)
		return
	}
	amount, err := t.binClient.CalculateBuyQty(t.Symbol, t.investmentPerTrade)
	if err != nil || amount <= 0 {
		fmt.Printf("[SKIP] [%s] Cannot buy: %v\n", t.Symbol, err)
		return
	}
	notional := amount * price
	if !t.demo {
		if err := t.binClient.MarketBuy(t.Symbol, amount); err != nil {
			fmt.Printf("[ERROR] [%s] Market Buy failed: %v\n", t.Symbol, err)
			return
		}
	}
	t.assetHeld += amount
	t.usdcBalance -= notional
	t.usdcInvested += notional
	t.averageBuyPrice = t.usdcInvested / t.assetHeld
	t.trailingHigh = price
	t.entries++
	t.holding = true

	if t.entries == 1 {
		t.se.LastBuyTime = time.Now()
	}
	if t.assetHeld < t.minHoldingThreshold {
		t.minHoldingThreshold = t.assetHeld * 0.9
		fmt.Printf("[ADJUST] [%s] Lowered minHoldingThreshold to %.8f\n", t.Symbol, t.minHoldingThreshold)
	}
	t.db.LogTransaction(t.Symbol, "BUY", amount, price)
	fmt.Printf("[TRADE] [%s] Bought at %.2f (%.2f USDC)\n", t.Symbol, price, notional)
}

func (t *Trader) canSell(price float64, history []float64) bool {
	if !t.holding || t.assetHeld <= 0 {
		return false
	}
	value := t.assetHeld * price
	if value < 5 {
		fmt.Printf("[SKIP] [%s] Position value %.2f below threshold\n", t.Symbol, value)
		return false
	}

	buyPrice := t.averageBuyPrice
	holdingTime := time.Since(t.se.LastBuyTime)
	if holdingTime < t.minHoldDuration {
		fmt.Printf("[SKIP] [%s] Holding %.0fs < minHold %.0fs\n", t.Symbol, holdingTime.Seconds(), t.minHoldDuration.Seconds())
		return false
	}

	if price > t.trailingHigh {
		t.trailingHigh = price
	}
	trailingStop := t.trailingHigh * (1 - t.trailingStopPct)

	commission := t.se.CommissionRate
	grossProfit := (price - buyPrice) / buyPrice
	netProfit := ((price * (1 - commission)) - (buyPrice * (1 + commission))) / buyPrice

	fmt.Printf("[CHECK] [%s] Gross=%.4f%%, Net=%.4f%%, RSI=%.2f, Price=%.2f, High=%.2f, Stop=%.2f\n",
		t.Symbol, grossProfit*100, netProfit*100, t.se.LastRSI, price, t.trailingHigh, trailingStop)

	if netProfit < t.se.MinProfitMargin {
		fmt.Printf("[SKIP] [%s] Net profit %.4f%% below target %.4f%%\n", t.Symbol, netProfit*100, t.se.MinProfitMargin*100)
		return false
	}

	if t.se.LastRSI > 85 && grossProfit > 0.001 {
		fmt.Printf("[TRIGGER] [%s] RSI>85 and profit>0.1%%\n", t.Symbol)
		return true
	}

	return price < trailingStop || t.se.ShouldSell(price, buyPrice, history)
}

func (t *Trader) trySell(price float64) {
	sellAmount := roundQuantity(t.assetHeld, 0.000001)
	if sellAmount <= 0 {
		fmt.Printf("[SKIP] [%s] Sell amount too small\n", t.Symbol)
		return
	}

	usdcReturn := sellAmount * price
	buyPrice := t.averageBuyPrice
	commission := t.se.CommissionRate

	grossProfit := (price - buyPrice) / buyPrice
	netProfit := ((price * (1 - commission)) - (buyPrice * (1 + commission))) / buyPrice
	holdingTime := time.Since(t.se.LastBuyTime)

	if !t.demo {
		if err := t.binClient.MarketSell(t.Symbol, sellAmount); err != nil {
			fmt.Printf("[ERROR] [%s] Market Sell failed: %v\n", t.Symbol, err)
			return
		}
	}

	t.assetHeld = 0
	t.holding = false
	t.usdcBalance += usdcReturn
	t.usdcProfit += usdcReturn - t.usdcInvested
	t.usdcInvested = 0
	t.averageBuyPrice = 0
	t.trailingHigh = 0
	t.entries = 0
	t.lastSellTime = time.Now()
	t.lastSellPrice = price

	t.db.LogTransaction(t.Symbol, "SELL", sellAmount, price)

	fmt.Printf("[TRADE] [%s] Sold at %.2f | Amount: %.6f | USDC: %.2f\n", t.Symbol, price, sellAmount, usdcReturn)
	fmt.Printf(" - Gross profit: %.4f%% | Net: %.4f%% | Held: %.0f minutes\n",
		grossProfit*100, netProfit*100, holdingTime.Minutes())
}

func (t *Trader) resetState() {
	t.assetHeld = 0
	t.holding = false
	t.averageBuyPrice = 0
	t.trailingHigh = 0
	t.entries = 0
	t.usdcInvested = 0
}

func (t *Trader) totalValue(price float64) float64 {
	return t.usdcBalance + t.assetHeld*price
}

func roundQuantity(quantity float64, step float64) float64 {
	return math.Floor(quantity/step) * step
}

func (t *Trader) Summary(price float64) map[string]float64 {
	unrealized := t.assetHeld * price
	return map[string]float64{
		"assetHeld":         t.assetHeld,
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
