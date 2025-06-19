# TradeRider

TradeRider is an intelligent, real-time trading bot with a visual monitoring dashboard, built for Binance. It combines a rule-based strategy engine, live market data, and an automated decision system to optimize trading performance.

## Features

- Real-time trading using EMA, RSI, and Bollinger Bands
- Automatic buy/sell execution with trailing stops and DCA (dollar-cost averaging)
- Customizable risk management parameters (stop-loss, cooldown, thresholds)
- Persistent logging of transactions in SQLite
- Live web dashboard:
    - Real-time asset summary
    - Historical trade list
    - Interactive chart with price movements and trade markers

## Architecture

```
main.go
├── config/          # YAML-based configuration
├── internal/
│   ├── api/         # HTTP API and web dashboard
│   ├── trader/      # Core trading logic
│   ├── strategy/    # Technical indicators and strategy logic
│   ├── market/      # Price polling and chart data
│   ├── store/       # SQLite wrapper for trade history
│   └── binance/     # Binance client integration
└── web/             # Static dashboard files (HTML/CSS/JS)
```

## Getting Started

### 1. Clone the repository

```bash
git clone https://github.com/your-org/traderider.git
cd traderider
```

### 2. Configure the bot

Create a configuration file at `config/config.yml`:

```yaml
mode: demo  # or "real"

binance:
  api_key: YOUR_API_KEY
  secret_key: YOUR_SECRET_KEY
  use_testnet: false

strategy:
  investment_per_trade: 20
  min_holding_threshold: 0.0002
  min_hold_minutes: 2
  max_holding_minutes: 120
  min_profit_margin: 0.005
  soft_stop_loss: 0.02
  min_trade_gap_percent: 0.003
```

### 3. Run the bot

```bash
go run main.go
```

The dashboard will be available at:

```
http://localhost:8080
```


