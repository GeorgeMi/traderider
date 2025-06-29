package wallet

import (
	"fmt"
	"log"
	"sync"
	"traderider/internal/binance"
	"traderider/internal/notifier"
)

type WalletManager struct {
	mu       sync.Mutex
	USDC     float64
	Demo     bool
	Client   *binance.Client
	notifier *notifier.WhatsAppNotifier
}

func NewWalletManager(demo bool, client *binance.Client, notifier *notifier.WhatsAppNotifier) *WalletManager {
	return &WalletManager{
		Demo:     demo,
		Client:   client,
		notifier: notifier,
	}
}

func (w *WalletManager) Update() {
	if w.Demo {
		return
	}
	usdc, err := w.Client.GetUSDCBalance()
	if err != nil {
		log.Printf("[WALLET] Failed to fetch USDC balance: %v", err)
		w.notifier.Send(fmt.Sprintf("[WALLET] Failed to fetch USDC balance: %v", err))
		return
	}
	w.mu.Lock()
	w.USDC = usdc
	w.mu.Unlock()
}

func (w *WalletManager) Reserve(amount float64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if amount > w.USDC {
		return false
	}
	w.USDC -= amount
	return true
}

func (w *WalletManager) Release(amount float64) {
	w.mu.Lock()
	w.USDC += amount
	w.mu.Unlock()
}

func (w *WalletManager) Balance() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.USDC
}
