# TradeRider

TradeRider is a real-time, intelligent trading bot for Binance, designed for autonomous portfolio growth using rule-based strategies and adaptive risk management. It features a live dashboard, persistent state, multi-symbol support, and advanced performance tracking.

## Features

- Live trading on Binance with real API (or demo mode)
- Advanced strategy: EMA crossover, RSI, Bollinger Bands, dynamic trailing stop, DCA
- Performance scoring per symbol with win rate, profit/loss analysis and rebalancing
- Risk management: soft stop loss, holding duration limits, cooldown, hard-stop if portfolio drops >10%
- Auto-rebalancing: reallocates capital based on performance score
- Force-sell button for each symbol
- Persistent state: survives restarts, resumes from saved trades
- Visual dashboard:
  - Real-time chart with BUY/SELL markers
  - Wallet breakdown and total value
  - Performance view (per-symbol stats and scores)

## Architecture

```
main.go
├── config/          # YAML configuration loader
├── internal/
│   ├── api/         # HTTP API, dashboard, performance, rebalancing
│   ├── trader/      # Trading loop, state machine, logic per symbol
│   ├── strategy/    # EMA, RSI, Bollinger Bands, scoring engine
│   ├── market/      # Real-time price fetcher and price history
│   ├── store/       # SQLite wrapper for transaction logs
│   ├── binance/     # Binance client, filters, real order execution
│   └── wallet/      # USDC tracking and reserve management
└── web/             # HTML/CSS/JS static frontend (dashboard)
```

## Getting Started

### 1. Clone the repository

```bash
git clone https://github.com/your-org/traderider.git
cd traderider
```

### 2. Configure the bot

Edit or create a config file at `config/config.yml`:

```yaml
mode: real  # or "demo"

binance:
  api_key: YOUR_API_KEY
  secret_key: YOUR_SECRET_KEY
  use_testnet: false

strategy:
  short_ema: 9
  long_ema: 21
  investment_per_trade: 20
  min_holding_threshold: 0.0002
  min_hold_minutes: 3
  max_holding_minutes: 45
  min_profit_margin: 0.007
  soft_stop_loss: 0.01
  min_trade_gap_percent: 0.003
  use_bollinger: true
  bollinger_window: 20
  commission_rate: 0.001

whatsapp:
  phone: YOUR_PHONE
  apikey: YOUR_API_KEY
```

### 3. Run the bot

```bash
go run main.go
```

### 4. Open the dashboard

```
http://localhost:1010
```

## API Endpoints

- /api/summary/{symbol} — live snapshot per asset
- /api/transactions/{symbol} — trade history
- /api/chart-data/{symbol} — price and trades over time
- /api/performance — full performance table (score, win rate, avg profit/loss)
- /api/wallet — total USDC wallet value
- /api/force-sell/{symbol} — forces instant liquidation
- /api/rebalance — triggers manual rebalancing

## Notes

- SQLite used for persistent storage
- Only supports USDC quote pairs (e.g., BTCUSDC, SOLUSDC)
- WhatsApp error notifications via CallMeBot integration

## Future (Planned)

- Backtesting engine with CSV input
- ML-based adaptive strategy scoring
- Portfolio heatmap and signal reasoning
- Realtime AI trade advisor (WIP)

---

TradeRider is designed for educational and experimental use. Use with caution in real markets.
