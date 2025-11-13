package trader

import (
	"fmt"
	"math"
	"nofx/market"
)

const (
	atrTrailingMultiplier = 2.0
	atr1HPeriod           = 14
)

func (m *TrailingStopMonitor) calculateATRTrailingStop(pos *positionSnapshot, riskDistance float64) (float64, string, error) {
	data, err := market.Get(pos.Symbol)
	if err != nil {
		return 0, "", fmt.Errorf("获取市场数据失败: %w", err)
	}

	var atr float64
	if data != nil && len(data.Klines1h) > 0 {
		atr = calculateATRFromKlines(data.Klines1h, atr1HPeriod)
	}

	if atr <= 0 {
		return 0, "", fmt.Errorf("1H ATR14 数据不可用")
	}

	var newStop float64
	if pos.Side == "long" {
		newStop = pos.MarkPrice - atr*atrTrailingMultiplier
		minStop := pos.EntryPrice + riskDistance // 保持 ≥ +1R
		if newStop < minStop {
			newStop = minStop
		}
	} else {
		newStop = pos.MarkPrice + atr*atrTrailingMultiplier
		maxStop := pos.EntryPrice - riskDistance
		if newStop > maxStop {
			newStop = maxStop
		}
	}

	reason := fmt.Sprintf(
		"ATR Trailing: ATR(1H,14)=%.4f × %.2f → 止损 %.4f",
		atr, atrTrailingMultiplier, newStop,
	)
	return newStop, reason, nil
}

func calculateATRFromKlines(klines []market.Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	trs := make([]float64, len(klines))
	for i := 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close

		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)

		trs[i] = math.Max(tr1, math.Max(tr2, tr3))
	}

	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trs[i]
	}
	atr := sum / float64(period)

	for i := period + 1; i < len(klines); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}
