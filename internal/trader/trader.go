package trader

import (
	"fmt"
	"math"
	"strings"
	"time"

	"traderider/internal/binance"
	"traderider/internal/market"
	"traderider/internal/notifier"
	"traderider/internal/store"
	"traderider/internal/strategy"
	"traderider/internal/wallet"
)

type Trader struct {
	Symbol              string
	db                  *store.Store
	mw                  *market.MarketWatcher
	se                  *strategy.StrategyEngine
	binClient           *binance.Client
	wallet              *wallet.WalletManager
	demo                bool
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
	minSellInterval     time.Duration
	lastSellPrice       float64
	lastSellProfit      float64
	minHoldingThreshold float64
	minHoldDuration     time.Duration
	stopCh              chan struct{}
	notifier            *notifier.WhatsAppNotifier
}

func NewTrader(db *store.Store, mw *market.MarketWatcher, se *strategy.StrategyEngine, demo bool, investmentPerTrade float64, binClient *binance.Client, minHoldingThreshold float64, minHoldDuration time.Duration, wallet *wallet.WalletManager, stopCh chan struct{}, notifier *notifier.WhatsAppNotifier) *Trader {
	return &Trader{
		db:                  db,
		mw:                  mw,
		se:                  se,
		binClient:           binClient,
		wallet:              wallet,
		demo:                demo,
		investmentPerTrade:  investmentPerTrade,
		holding:             false,
		trailingStopPct:     0.02,
		maxEntries:          3,
		cooldownDuration:    90 * time.Second,
		minSellInterval:     12 * time.Minute,
		minHoldingThreshold: minHoldingThreshold,
		minHoldDuration:     minHoldDuration,
		stopCh:              stopCh,
		notifier:            notifier,
	}
}

func (t *Trader) Run() {
	for {
		select {
		case <-t.stopCh:
			fmt.Printf("[STOPPED] Trader for %s stopped by hard stop\n", t.Symbol)
			return
		default:
		}

		time.Sleep(5 * time.Second)
		t.updateBalances()
		if t.dailyStartValue == 0 {
			continue
		}

		price := t.mw.GetPrice(t.Symbol)
		history := t.mw.GetHistory(t.Symbol)

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

func (t *Trader) inCooldown() bool {
	if time.Since(t.lastSellTime) < t.cooldownDuration || t.lastSellProfit < -0.3 {
		fmt.Printf("[COOLDOWN] [%s] %.0fs remaining\n", t.Symbol, (t.cooldownDuration - time.Since(t.lastSellTime)).Seconds())
		return true
	}
	return false
}

func (t *Trader) canBuy(price float64, history []float64) bool {
	if !t.wallet.Reserve(t.investmentPerTrade) {
		return false
	}

	if t.holding && (t.entries >= t.maxEntries || price > t.averageBuyPrice*0.96) {
		t.wallet.Release(t.investmentPerTrade)
		return false
	}

	// Așteaptă o scădere semnificativă după SELL
	if !t.holding && t.lastSellPrice > 0 && price > t.lastSellPrice*(1-0.005) {
		t.wallet.Release(t.investmentPerTrade)
		fmt.Printf("[WAIT] [%s] Waiting for price to drop after last SELL (%.2f → %.2f)\n", t.Symbol, t.lastSellPrice, price)
		return false
	}

	// Confirmă formarea unui bottom local
	/*if !confirmBottomFormation(history) {
		t.wallet.Release(t.investmentPerTrade)
		fmt.Printf("[SKIP] [%s] No bottom pattern detected\n", t.Symbol)
		return false
	}*/

	// Bollinger Bands: cumpără doar în zona inferioară
	if t.se.UseBollinger {
		lower, _, _ := strategy.CalculateBollingerBands(history, t.se.BollingerWindow)
		if price > lower*1.02 {
			t.wallet.Release(t.investmentPerTrade)
			fmt.Printf("[SKIP] [%s] Price not low enough in Bollinger band (%.2f > %.2f)\n", t.Symbol, price, lower*1.02)
			return false
		}
	}

	// RSI: trebuie să fie destul de jos
	rsi := t.se.CalculateRSI(history)
	if rsi > 40 {
		t.wallet.Release(t.investmentPerTrade)
		fmt.Printf("[SKIP] [%s] RSI too high for buy: %.2f\n", t.Symbol, rsi)
		return false
	}

	// Spread verificare
	spread := t.binClient.GetSpread(t.Symbol)
	if spread > 0.002 {
		t.wallet.Release(t.investmentPerTrade)
		fmt.Printf("[SKIP] [%s] Spread too high: %.4f\n", t.Symbol, spread)
		return false
	}

	return true
}

func (t *Trader) tryBuy(price float64) {
	amount, err := t.binClient.CalculateBuyQty(t.Symbol, t.investmentPerTrade)
	if err != nil || amount <= 0 {
		t.wallet.Release(t.investmentPerTrade)
		//t.notifier.Send(fmt.Sprintf("[BUY ERROR] [%s] CalculateBuyQty failed: %v", t.Symbol, err))
		return
	}

	executedPrice := price
	if !t.demo {
		executedPrice, err = t.binClient.MarketBuy(t.Symbol, amount)
		if err != nil {
			t.wallet.Release(t.investmentPerTrade)
			t.notifier.Send(fmt.Sprintf("[ERROR] [%s] MarketBuy failed: %v", t.Symbol, err))
			return
		}
	}

	notional := amount * executedPrice
	t.assetHeld += amount
	t.usdcInvested += notional
	t.averageBuyPrice = t.usdcInvested / t.assetHeld
	t.trailingHigh = executedPrice
	t.entries++
	t.holding = true
	if t.entries == 1 {
		t.se.LastBuyTime = time.Now()
	}
	t.db.LogTransaction(t.Symbol, "BUY", amount, executedPrice)
	fmt.Printf("[TRADE] [%s] Bought at %.2f (%.2f USDC)\n", t.Symbol, executedPrice, notional)
}

func (t *Trader) canSell(price float64, history []float64) bool {
	if !t.holding || t.assetHeld <= 0 {
		return false
	}

	balance, _ := t.binClient.GetAssetBalance(strings.Replace(t.Symbol, "USDC", "", 1))
	if balance == 0 {
		t.resetState()
		return false
	}

	holdingTime := time.Since(t.se.LastBuyTime)
	if holdingTime < t.minHoldDuration || holdingTime < t.minSellInterval {
		return false
	}

	if price > t.trailingHigh {
		t.trailingHigh = price
	}

	commission := t.se.CommissionRate
	netProfit := ((price * (1 - commission)) - (t.averageBuyPrice * (1 + commission))) / t.averageBuyPrice

	if netProfit < t.se.MinProfitMargin {
		return false
	}

	spread := t.binClient.GetSpread(t.Symbol)
	if spread > 0.002 {
		fmt.Printf("[SKIP] [%s] Spread too high: %.4f\n", t.Symbol, spread)
		return false
	}

	if price < t.trailingHigh*(1-t.dynamicTrailingStop(netProfit)) && t.confirmDownTrend(history) {
		return true
	}

	return t.se.ShouldSell(price, t.averageBuyPrice, history)
}

func (t *Trader) trySell(price float64) {
	step := t.binClient.GetSymbolFilter(t.Symbol).StepSize
	sellAmount := roundQuantity(t.assetHeld, step)
	if sellAmount <= 0 {
		return
	}

	balance, _ := t.binClient.GetAssetBalance(strings.Replace(t.Symbol, "USDC", "", 1))
	if balance < sellAmount {
		fmt.Printf("[SKIP] [%s] Not enough balance to sell (have %.4f, need %.4f)", t.Symbol, balance, sellAmount)
		t.resetState()
		return
	}

	executedPrice := price
	var err error
	if !t.demo {
		executedPrice, err = t.binClient.MarketSell(t.Symbol, sellAmount)
		if err != nil {
			t.notifier.Send(fmt.Sprintf("[ERROR] [%s] MarketSell failed: %v", t.Symbol, err))
			return
		}
	}

	usdcReturn := sellAmount * executedPrice
	t.wallet.Release(usdcReturn)

	commission := t.se.CommissionRate
	netProfit := ((executedPrice * (1 - commission)) - (t.averageBuyPrice * (1 + commission))) / t.averageBuyPrice
	holdingTime := time.Since(t.se.LastBuyTime)

	t.assetHeld = 0
	t.holding = false
	t.usdcProfit += usdcReturn - t.usdcInvested
	t.usdcInvested = 0
	t.averageBuyPrice = 0
	t.trailingHigh = 0
	t.entries = 0
	t.lastSellTime = time.Now()
	t.lastSellPrice = executedPrice
	t.lastSellProfit = netProfit * 100

	t.db.LogTransaction(t.Symbol, "SELL", sellAmount, executedPrice)
	fmt.Printf("[TRADE] [%s] Sold at %.2f | NetProfit: %.2f%% | Held: %.0fmin\n", t.Symbol, executedPrice, netProfit*100, holdingTime.Minutes())
}

func (t *Trader) updateBalances() {
	if t.demo || t.dailyStartValue != 0 {
		return
	}

	asset := strings.Replace(t.Symbol, "USDC", "", 1)
	price := t.mw.GetPrice(t.Symbol)
	balance, err := t.binClient.GetAssetBalance(asset)
	if err != nil {
		fmt.Printf("[WARN] [%s] Cannot fetch balance for %s: %v\n", t.Symbol, asset, err)
		balance = 0
	}

	t.assetHeld = balance
	if t.assetHeld > 0 {
		if t.averageBuyPrice == 0 {
			t.averageBuyPrice = price
		}
		if t.usdcInvested == 0 {
			t.usdcInvested = t.assetHeld * t.averageBuyPrice
		}
		t.holding = true
		fmt.Printf("[SYNC] [%s] Resumed holding %.4f units\n", t.Symbol, t.assetHeld)
	}

	t.dailyStartValue = t.assetHeld*price + t.wallet.Balance()
	fmt.Printf("[INIT] [%s] Daily start set to %.2f (asset=%.2f, usdc=%.2f)\n",
		t.Symbol, t.dailyStartValue, t.assetHeld*price, t.wallet.Balance())
}

func (t *Trader) resetIfInvalid(price float64) {
	if t.holding && t.assetHeld > 0 && t.assetHeld < t.minHoldingThreshold {
		if t.assetHeld*price < 5 {
			fmt.Printf("[RESET] [%s] holding %.8f too low\n", t.Symbol, t.assetHeld)
			t.resetState()
		}
	}
}

func (t *Trader) resetState() {
	t.assetHeld = 0
	t.holding = false
	t.averageBuyPrice = 0
	t.trailingHigh = 0
	t.entries = 0
	t.usdcInvested = 0
}

func (t *Trader) confirmDownTrend(history []float64) bool {
	if len(history) < 3 {
		return false
	}
	return history[len(history)-1] < history[len(history)-2] &&
		history[len(history)-2] < history[len(history)-3]
}

func (t *Trader) dynamicTrailingStop(netProfit float64) float64 {
	switch {
	case netProfit >= 0.06:
		return 0.012
	case netProfit >= 0.03:
		return 0.015
	case netProfit >= 0.015:
		return 0.02
	case netProfit >= 0.01:
		return 0.025
	default:
		return 0.035
	}
}

func (t *Trader) Summary(price float64) map[string]float64 {
	unrealized := t.assetHeld * price
	return map[string]float64{
		"assetHeld":         t.assetHeld,
		"usdcInvested":      t.usdcInvested,
		"usdcProfit":        t.usdcProfit,
		"unrealized":        unrealized,
		"totalValue":        t.totalValue(price),
		"averageBuyPrice":   t.averageBuyPrice,
		"usdcInvestedTotal": t.usdcProfit + unrealized,
		"usdcBalance":       t.wallet.Balance(),
	}
}

type StateSnapshot struct {
	AssetHeld       float64
	USDCInvested    float64
	AverageBuyPrice float64
	TrailingHigh    float64
	Entries         int
	Holding         bool
	LastSellPrice   float64
	LastSellTime    time.Time
}

func (t *Trader) SnapshotState() StateSnapshot {
	return StateSnapshot{
		AssetHeld:       t.assetHeld,
		USDCInvested:    t.usdcInvested,
		AverageBuyPrice: t.averageBuyPrice,
		TrailingHigh:    t.trailingHigh,
		Entries:         t.entries,
		Holding:         t.holding,
		LastSellPrice:   t.lastSellPrice,
		LastSellTime:    t.lastSellTime,
	}
}

func (t *Trader) RestoreState(s StateSnapshot) {
	t.assetHeld = s.AssetHeld
	t.usdcInvested = s.USDCInvested
	t.averageBuyPrice = s.AverageBuyPrice
	t.trailingHigh = s.TrailingHigh
	t.entries = s.Entries
	t.holding = s.Holding
	t.lastSellPrice = s.LastSellPrice
	t.lastSellTime = s.LastSellTime
}

func roundQuantity(quantity float64, step float64) float64 {
	return math.Floor(quantity/step) * step
}

func (t *Trader) totalValue(price float64) float64 {
	return t.wallet.Balance() + t.assetHeld*price
}

func (t *Trader) ForceSell() {
	price := t.mw.GetPrice(t.Symbol)
	step := t.binClient.GetSymbolFilter(t.Symbol).StepSize
	sellAmount := roundQuantity(t.assetHeld, step)

	if sellAmount <= 0 {
		fmt.Printf("[FORCESELL] [%s] Nothing to sell\n", t.Symbol)
		return
	}

	executedPrice := price
	var err error
	if !t.demo {
		executedPrice, err = t.binClient.MarketSell(t.Symbol, sellAmount)
		if err != nil {
			msg := fmt.Sprintf("[FORCESELL ERROR] [%s] MarketSell failed: %v", t.Symbol, err)
			fmt.Println(msg)
			t.notifier.Send(msg)
			return
		}
	}

	usdcReturn := sellAmount * executedPrice
	t.wallet.Release(usdcReturn)

	t.db.LogTransaction(t.Symbol, "SELL", sellAmount, executedPrice)

	t.assetHeld = 0
	t.holding = false
	t.usdcProfit += usdcReturn - t.usdcInvested
	t.usdcInvested = 0
	t.averageBuyPrice = 0
	t.trailingHigh = 0
	t.entries = 0
	t.lastSellTime = time.Now()
	t.lastSellPrice = executedPrice
	t.lastSellProfit = 0

	fmt.Printf("[FORCESELL] [%s] Sold %.4f at %.2f (%.2f USDC)\n", t.Symbol, sellAmount, executedPrice, usdcReturn)
}

func (t *Trader) SetInvestmentPerTrade(newAmount float64) {
	t.investmentPerTrade = newAmount
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
