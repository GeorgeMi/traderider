package api

import (
	"encoding/json"
	"math"
	"net/http"
	"time"
)

type PerformanceStats struct {
	TotalProfit float64 `json:"totalProfit"`
	TotalTrades int     `json:"totalTrades"`
	Wins        int     `json:"wins"`
	Losses      int     `json:"losses"`
	WinRate     float64 `json:"winRate"`
	AvgProfit   float64 `json:"avgProfit"`
	AvgLoss     float64 `json:"avgLoss"`
	Score       float64 `json:"score"`
}

func (s *Server) handlePerformance(w http.ResponseWriter, r *http.Request) {
	perf := make(map[string]PerformanceStats)

	for _, symbol := range s.TrackedPairs {
		rows, err := s.DB.Query(`
			SELECT side, amount, price, time FROM transactions
			WHERE symbol = ? ORDER BY time ASC
		`, symbol)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var stats PerformanceStats
		var buyStack []struct {
			amount float64
			price  float64
			time   time.Time
		}

		commission := 0.001 // Binance commission default
		var holdingDurations []float64
		for rows.Next() {
			var side string
			var price, amt float64
			var ts string
			_ = rows.Scan(&side, &amt, &price, &ts)
			timeParsed, _ := time.Parse("2006-01-02 15:04:05", ts)

			if side == "BUY" {
				buyStack = append(buyStack, struct {
					amount float64
					price  float64
					time   time.Time
				}{amt, price, timeParsed})
			} else if side == "SELL" && len(buyStack) > 0 {
				amtLeft := amt
				for len(buyStack) > 0 && amtLeft > 0 {
					buy := buyStack[0]
					qty := math.Min(amtLeft, buy.amount)

					buyPrice := buy.price * (1 + commission)
					sellPrice := price * (1 - commission)
					profit := (sellPrice - buyPrice) * qty

					stats.TotalProfit += profit
					stats.TotalTrades++
					if profit > 0 {
						stats.Wins++
						stats.AvgProfit += profit
					} else {
						stats.Losses++
						stats.AvgLoss += profit
					}

					holdingTime := timeParsed.Sub(buy.time).Minutes()
					holdingDurations = append(holdingDurations, holdingTime)

					amtLeft -= qty
					if qty < buy.amount {
						buyStack[0].amount -= qty
						break
					} else {
						buyStack = buyStack[1:]
					}
				}
			}
		}

		if stats.Wins > 0 {
			stats.AvgProfit /= float64(stats.Wins)
		}
		if stats.Losses > 0 {
			stats.AvgLoss /= float64(stats.Losses)
		}
		if stats.TotalTrades > 0 {
			stats.WinRate = float64(stats.Wins) / float64(stats.TotalTrades)
		}

		var avgHold float64
		for _, h := range holdingDurations {
			avgHold += h
		}
		if len(holdingDurations) > 0 {
			avgHold /= float64(len(holdingDurations))
		}

		// Score = profit/|loss| * winrate * trade_count / avgHold
		if stats.TotalTrades > 0 {
			lossAbs := math.Abs(stats.AvgLoss)
			denom := lossAbs + 0.01
			confidence := math.Log(float64(stats.TotalTrades) + 1)
			score := (stats.TotalProfit / denom) * stats.WinRate * confidence / (avgHold + 1)
			stats.Score = round(score, 2)
		}

		perf[symbol] = stats
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(perf)
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func round(val float64, precision int) float64 {
	pow := math.Pow(10, float64(precision))
	return math.Round(val*pow) / pow
}
