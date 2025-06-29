package binance

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"traderider/internal/notifier"

	binance "github.com/adshao/go-binance/v2"
)

type Client struct {
	api           *binance.Client
	symbolFilters map[string]SymbolFilter
	notifier      *notifier.WhatsAppNotifier
}

type SymbolFilter struct {
	MinQty      float64
	StepSize    float64
	MinNotional float64
}

func NewClient(apiKey, secretKey string, notifier *notifier.WhatsAppNotifier) *Client {
	c := binance.NewClient(apiKey, secretKey)
	client := &Client{
		api:           c,
		symbolFilters: make(map[string]SymbolFilter),
		notifier:      notifier,
	}
	client.loadSymbolFilters()
	return client
}

func (c *Client) loadSymbolFilters() {
	info, err := c.api.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		log.Printf("[ERROR] Failed to load exchange info: %v", err)
		c.notifier.Send(fmt.Sprintf("[ERROR] Failed to load exchange info: %v", err))
		return
	}
	for _, sym := range info.Symbols {
		var minQty, stepSize, minNotional float64
		for _, filter := range sym.Filters {
			switch filter["filterType"] {
			case "LOT_SIZE":
				minQty, _ = strconv.ParseFloat(filter["minQty"].(string), 64)
				stepSize, _ = strconv.ParseFloat(filter["stepSize"].(string), 64)
			case "NOTIONAL":
				minNotional, _ = strconv.ParseFloat(filter["minNotional"].(string), 64)
			}
		}
		c.symbolFilters[sym.Symbol] = SymbolFilter{MinQty: minQty, StepSize: stepSize, MinNotional: minNotional}
	}
}

func (c *Client) GetSymbolPrice(symbol string) float64 {
	res, err := c.api.NewListPricesService().Symbol(symbol).Do(context.Background())
	if err != nil || len(res) == 0 {
		log.Printf("[ERROR] Binance price error for %s: %v", symbol, err)
		c.notifier.Send(fmt.Sprintf("[ERROR] Binance price error for %s: %v", symbol, err))
		return 0
	}
	price, _ := strconv.ParseFloat(res[0].Price, 64)
	return price
}

func (c *Client) GetSpread(symbol string) float64 {
	orderBook, err := c.api.NewDepthService().Symbol(symbol).Limit(5).Do(context.Background())
	if err != nil || len(orderBook.Bids) == 0 || len(orderBook.Asks) == 0 {
		log.Printf("[ERROR] Failed to fetch order book: %v", err)
		return 9999
	}
	bid, _ := strconv.ParseFloat(orderBook.Bids[0].Price, 64)
	ask, _ := strconv.ParseFloat(orderBook.Asks[0].Price, 64)
	return (ask - bid) / bid
}

func (c *Client) GetAssetBalance(asset string) (float64, error) {
	account, err := c.api.NewGetAccountService().Do(context.Background())
	if err != nil {
		log.Printf("[ERROR] Binance account error: %v", err)
		c.notifier.Send(fmt.Sprintf("[ERROR] Binance account error: %v", err))
		return 0, err
	}
	for _, b := range account.Balances {
		if b.Asset == asset {
			return strconv.ParseFloat(b.Free, 64)
		}
	}
	return 0, nil
}

func (c *Client) GetUSDCBalance() (float64, error) {
	return c.GetAssetBalance("USDC")
}

func (c *Client) adjustQuantity(symbol string, quantity float64) float64 {
	filter := c.GetSymbolFilter(symbol)
	if filter.StepSize == 0 {
		return quantity
	}
	steps := math.Floor(quantity / filter.StepSize)
	adj := steps * filter.StepSize
	if adj < filter.MinQty {
		log.Printf("[ADJUST] Quantity %.8f < MinQty %.8f", adj, filter.MinQty)
	}
	return adj
}

func (c *Client) GetSymbolFilter(symbol string) SymbolFilter {
	if filter, ok := c.symbolFilters[symbol]; ok {
		return filter
	}
	return SymbolFilter{MinQty: 0.00001, StepSize: 0.00000001, MinNotional: 10.0}
}

func (c *Client) MarketBuy(symbol string, quantity float64) (float64, error) {
	quantity = c.adjustQuantity(symbol, quantity)
	if quantity <= 0 {
		return 0, fmt.Errorf("invalid quantity for MarketBuy: %s", symbol)
	}
	order, err := c.api.NewCreateOrderService().Symbol(symbol).Side(binance.SideTypeBuy).
		Type(binance.OrderTypeMarket).Quantity(fmt.Sprintf("%.8f", quantity)).Do(context.Background())
	if err != nil {
		return 0, err
	}
	return parseFillPrice(order.Fills)
}

func (c *Client) MarketSell(symbol string, quantity float64) (float64, error) {
	quantity = c.adjustQuantity(symbol, quantity)
	if quantity <= 0 {
		return 0, fmt.Errorf("invalid quantity for MarketSell: %s", symbol)
	}
	order, err := c.api.NewCreateOrderService().Symbol(symbol).Side(binance.SideTypeSell).
		Type(binance.OrderTypeMarket).Quantity(fmt.Sprintf("%.8f", quantity)).Do(context.Background())
	if err != nil {
		return 0, err
	}
	return parseFillPrice(order.Fills)
}

func parseFillPrice(fills []*binance.Fill) (float64, error) {
	totalPrice, totalQty := 0.0, 0.0
	for _, f := range fills {
		price, _ := strconv.ParseFloat(f.Price, 64)
		qty, _ := strconv.ParseFloat(f.Quantity, 64)
		totalPrice += price * qty
		totalQty += qty
	}
	if totalQty == 0 {
		return 0, fmt.Errorf("empty fills")
	}
	return totalPrice / totalQty, nil
}

func (c *Client) CalculateBuyQty(symbol string, availableUSDC float64) (float64, error) {
	price := c.GetSymbolPrice(symbol)
	if price == 0 {
		return 0, fmt.Errorf("price unavailable")
	}
	filter := c.GetSymbolFilter(symbol)
	minQty := filter.MinNotional / price
	minQty = c.adjustQuantity(symbol, minQty)
	if availableUSDC < minQty*price {
		return 0, fmt.Errorf("not enough USDC")
	}
	qty := availableUSDC / price
	qty = c.adjustQuantity(symbol, qty)
	if qty*price < filter.MinNotional {
		return 0, fmt.Errorf("notional too low")
	}
	return qty, nil
}

func (c *Client) DebugPrintFilters() {
	for symbol, filter := range c.symbolFilters {
		fmt.Printf("%s → minQty=%.8f, stepSize=%.8f, minNotional=%.2f\n",
			symbol, filter.MinQty, filter.StepSize, filter.MinNotional)
	}
}
