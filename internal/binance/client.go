package binance

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"

	binance "github.com/adshao/go-binance/v2"
)

type Client struct {
	api           *binance.Client
	symbolFilters map[string]SymbolFilter
}

type SymbolFilter struct {
	MinQty      float64
	StepSize    float64
	MinNotional float64
}

func NewClient(apiKey, secretKey string) *Client {
	c := binance.NewClient(apiKey, secretKey)
	client := &Client{
		api:           c,
		symbolFilters: make(map[string]SymbolFilter),
	}
	client.loadSymbolFilters()
	return client
}

func (c *Client) loadSymbolFilters() {
	info, err := c.api.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		log.Printf("[ERROR] Failed to load exchange info: %v", err)
		return
	}
	for _, sym := range info.Symbols {
		var minQty, stepSize, minNotional float64
		for _, filter := range sym.Filters {
			switch filter["filterType"] {
			case "LOT_SIZE":
				if minQtyStr, ok := filter["minQty"].(string); ok {
					minQty, _ = strconv.ParseFloat(minQtyStr, 64)
				}
				if stepSizeStr, ok := filter["stepSize"].(string); ok {
					stepSize, _ = strconv.ParseFloat(stepSizeStr, 64)
				}
			case "NOTIONAL":
				if notionalStr, ok := filter["minNotional"].(string); ok {
					minNotional, _ = strconv.ParseFloat(notionalStr, 64)
				}
			}
		}
		c.symbolFilters[sym.Symbol] = SymbolFilter{
			MinQty:      minQty,
			StepSize:    stepSize,
			MinNotional: minNotional,
		}
	}
}

func (c *Client) GetSymbolPrice(symbol string) float64 {
	res, err := c.api.NewListPricesService().Symbol(symbol).Do(context.Background())
	if err != nil || len(res) == 0 {
		log.Printf("[ERROR] Binance price error for %s: %v", symbol, err)
		return 0
	}
	price, _ := strconv.ParseFloat(res[0].Price, 64)
	return price
}

func (c *Client) GetAssetBalance(asset string) (float64, error) {
	account, err := c.api.NewGetAccountService().Do(context.Background())
	if err != nil {
		log.Printf("[ERROR] Binance account error: %v", err)
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
	filter, ok := c.symbolFilters[symbol]
	if !ok || filter.StepSize == 0 {
		return quantity
	}
	rawSteps := quantity / filter.StepSize
	if rawSteps < 1 {
		return 0
	}
	adjusted := math.Floor(rawSteps) * filter.StepSize
	if adjusted < filter.MinQty {
		log.Printf("[ADJUST] Quantity %.8f is below MinQty %.8f, proceeding anyway", adjusted, filter.MinQty)
	}
	return adjusted
}

func (c *Client) GetSymbolFilter(symbol string) SymbolFilter {
	if filter, ok := c.symbolFilters[symbol]; ok {
		return filter
	}
	return SymbolFilter{
		MinQty:      0.00001000,
		StepSize:    0.00000001,
		MinNotional: 10.0,
	}
}

func (c *Client) MarketBuy(symbol string, quantity float64) error {
	quantity = c.adjustQuantity(symbol, quantity)
	if quantity <= 0 {
		return fmt.Errorf("quantity after adjustment is 0 or below minQty for symbol %s", symbol)
	}
	order, err := c.api.NewCreateOrderService().
		Symbol(symbol).
		Side(binance.SideTypeBuy).
		Type(binance.OrderTypeMarket).
		Quantity(fmt.Sprintf("%.8f", quantity)).
		Do(context.Background())
	if err != nil {
		return err
	}
	log.Printf("Market Buy Order executed: %+v", order)
	return nil
}

func (c *Client) MarketSell(symbol string, quantity float64) error {
	quantity = c.adjustQuantity(symbol, quantity)
	if quantity <= 0 {
		return fmt.Errorf("quantity after adjustment is 0 or below minQty for symbol %s", symbol)
	}
	order, err := c.api.NewCreateOrderService().
		Symbol(symbol).
		Side(binance.SideTypeSell).
		Type(binance.OrderTypeMarket).
		Quantity(fmt.Sprintf("%.8f", quantity)).
		Do(context.Background())
	if err != nil {
		return err
	}
	log.Printf("Market Sell Order executed: %+v", order)
	return nil
}

func (c *Client) CalculateBuyQty(symbol string, availableUSDC float64) (float64, error) {
	price := c.GetSymbolPrice(symbol)
	if price == 0 {
		return 0, fmt.Errorf("price unavailable")
	}
	filter := c.GetSymbolFilter(symbol)
	minQty := filter.MinNotional / price
	minQty = c.adjustQuantity(symbol, minQty)
	requiredUSDC := minQty * price
	if availableUSDC < requiredUSDC {
		log.Printf("[BUY_QTY] Not enough USDC: have %.2f, need %.2f for minQty %.8f", availableUSDC, requiredUSDC, minQty)
		return 0, fmt.Errorf("not enough USDC: need %.2f for minQty", requiredUSDC)
	}
	qty := availableUSDC / price
	qty = c.adjustQuantity(symbol, qty)
	notional := price * qty
	if notional < filter.MinNotional {
		log.Printf("[BUY_QTY] Notional too low: %.2f < %.2f", notional, filter.MinNotional)
		return 0, fmt.Errorf("notional %.2f below minimum %.2f", notional, filter.MinNotional)
	}
	return qty, nil
}

func (c *Client) DebugPrintFilters() {
	for symbol, filter := range c.symbolFilters {
		fmt.Printf("%s → minQty=%.8f, stepSize=%.8f, minNotional=%.2f\n",
			symbol, filter.MinQty, filter.StepSize, filter.MinNotional)
	}
}
