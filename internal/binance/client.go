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
	api *binance.Client

	// Cache pentru filtrele LOT_SIZE per simbol
	symbolFilters map[string]lotSizeFilter
}

type lotSizeFilter struct {
	MinQty   float64
	StepSize float64
}

func NewClient(apiKey, secretKey string) *Client {
	c := binance.NewClient(apiKey, secretKey)
	client := &Client{
		api:           c,
		symbolFilters: make(map[string]lotSizeFilter),
	}
	client.loadSymbolFilters() // încarcă filtrele la pornire
	return client
}

// Încarcă filtrele LOT_SIZE din exchange info și le pune în cache
func (c *Client) loadSymbolFilters() {
	info, err := c.api.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		log.Printf("[ERROR] Failed to load exchange info: %v", err)
		return
	}

	for _, sym := range info.Symbols {
		for _, filter := range sym.Filters {
			if filter["filterType"] == "LOT_SIZE" {
				minQtyStr, _ := filter["minQty"].(string)
				stepSizeStr, _ := filter["stepSize"].(string)
				minQty, err1 := strconv.ParseFloat(minQtyStr, 64)
				stepSize, err2 := strconv.ParseFloat(stepSizeStr, 64)
				if err1 == nil && err2 == nil {
					c.symbolFilters[sym.Symbol] = lotSizeFilter{
						MinQty:   minQty,
						StepSize: stepSize,
					}
				}
			}
		}
	}
}

// Ajustează cantitatea conform pasului (stepSize) și minQty din filtre
func (c *Client) adjustQuantity(symbol string, quantity float64) float64 {
	filter, ok := c.symbolFilters[symbol]
	if !ok {
		// Daca nu avem info de filtru, returnam cantitatea originala
		return quantity
	}

	// Rotunjim în jos la cel mai apropiat multiplu de stepSize
	adjusted := math.Floor(quantity/filter.StepSize) * filter.StepSize

	// Dacă după ajustare este sub minQty, cantitatea nu este validă (returnăm 0)
	if adjusted < filter.MinQty {
		return 0
	}

	return adjusted
}

func (c *Client) GetBTCPrice() float64 {
	res, err := c.api.NewListPricesService().Symbol("BTCUSDC").Do(context.Background())
	if err != nil || len(res) == 0 {
		log.Printf("[ERROR] Binance price error: %v", err)
		return 0
	}
	price, _ := strconv.ParseFloat(res[0].Price, 64)
	return price
}

func (c *Client) GetUSDCBalance() (float64, error) {
	account, err := c.api.NewGetAccountService().Do(context.Background())
	if err != nil {
		log.Printf("[ERROR] Binance account error: %v", err)
		return 0, err
	}

	for _, b := range account.Balances {
		if b.Asset == "USDC" {
			free, err := strconv.ParseFloat(b.Free, 64)
			if err != nil {
				return 0, err
			}
			return free, nil
		}
	}
	return 0, nil
}

func (c *Client) MarketBuy(symbol string, quantity float64) error {
	quantity = c.adjustQuantity(symbol, quantity)
	if quantity == 0 {
		return fmt.Errorf("quantity after adjustment is 0 or below minQty for symbol %s", symbol)
	}

	order, err := c.api.NewCreateOrderService().
		Symbol(symbol).
		Side(binance.SideTypeBuy).
		Type(binance.OrderTypeMarket).
		Quantity(fmt.Sprintf("%.6f", quantity)).
		Do(context.Background())
	if err != nil {
		return err
	}
	log.Printf("Market Buy Order executed: %+v", order)
	return nil
}

func (c *Client) MarketSell(symbol string, quantity float64) error {
	quantity = c.adjustQuantity(symbol, quantity)
	if quantity == 0 {
		return fmt.Errorf("quantity after adjustment is 0 or below minQty for symbol %s", symbol)
	}

	order, err := c.api.NewCreateOrderService().
		Symbol(symbol).
		Side(binance.SideTypeSell).
		Type(binance.OrderTypeMarket).
		Quantity(fmt.Sprintf("%.6f", quantity)).
		Do(context.Background())
	if err != nil {
		return err
	}
	log.Printf("Market Sell Order executed: %+v", order)
	return nil
}

func (c *Client) GetBTCBalance() (float64, error) {
	account, err := c.api.NewGetAccountService().Do(context.Background())
	if err != nil {
		return 0, err
	}

	for _, b := range account.Balances {
		if b.Asset == "BTC" {
			return strconv.ParseFloat(b.Free, 64)
		}
	}
	return 0, nil
}
