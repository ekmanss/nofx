package trailingstop

import (
	"fmt"
	"math"
	"nofx/market"
	"strings"
)

const (
	atr1HPeriod         = 14
	phaseStartBreakeven = 1.5
)

// RiskSnapshot is a lightweight view of the information needed to compute the trailing stop.
type RiskSnapshot struct {
	InitialStop float64
	PeakPrice   float64
}

// ATRFetcher allows tests to provide deterministic ATR data.
type ATRFetcher func(symbol string) (float64, error)

// ATRTrailingCalculator encapsulates the ATR-based trailing stop rules.
type ATRTrailingCalculator struct {
	fetchATR ATRFetcher
}

// NewATRTrailingCalculator creates a calculator with the provided ATR fetcher.
// If fetcher is nil, the calculator will pull ATR14 from the market package directly.
func NewATRTrailingCalculator(fetcher ATRFetcher) *ATRTrailingCalculator {
	if fetcher == nil {
		fetcher = fetchOneHourATR
	}
	return &ATRTrailingCalculator{fetchATR: fetcher}
}

// Calculate returns the next stop price together with a human readable explanation.
func (c *ATRTrailingCalculator) Calculate(
	pos *Snapshot,
	risk *RiskSnapshot,
	prevStop float64,
	hasPrevStop bool,
) (float64, string, error) {
	if pos == nil {
		return 0, "", fmt.Errorf("持仓信息缺失")
	}
	if risk == nil {
		return 0, "", fmt.Errorf("风险信息缺失")
	}

	entry := pos.EntryPrice
	mark := pos.MarkPrice
	riskDistance := math.Abs(entry - risk.InitialStop)
	if riskDistance <= 0 {
		return 0, "", fmt.Errorf("风险距离无效")
	}

	currentR := currentRMultiple(pos.Side, entry, mark, riskDistance)
	baseStop := risk.InitialStop
	if hasPrevStop {
		baseStop = prevStop
	}

	if currentR < phaseStartBreakeven {
		return baseStop, fmt.Sprintf("阶段0：<1.5R，保持止损 %.4f", baseStop), nil
	}

	if currentR < 3.0 {
		target := entry
		label := "阶段1：保本"
		var candidate float64
		if pos.Side == "long" {
			candidate = tightenStopLong(baseStop, target)
		} else {
			candidate = tightenStopShort(baseStop, target)
		}
		suffix := ""
		if nearEqual(candidate, baseStop) {
			suffix = "（保持现有止损）"
		}
		return candidate, fmt.Sprintf("%s → %.4f%s", label, candidate, suffix), nil
	}

	atr, err := c.fetchATR(pos.Symbol)
	if err != nil {
		return 0, "", err
	}
	if atr <= 0 {
		return 0, "", fmt.Errorf("1H ATR14 数据不可用")
	}

	regimeVol := atr / mark
	assetClass := classifyAsset(pos.Symbol)

	if pos.Side == "long" {
		return calculateDynamicStopLong(
			entry,
			mark,
			baseStop,
			risk,
			currentR,
			atr,
			regimeVol,
			assetClass,
		)
	}

	return calculateDynamicStopShort(
		entry,
		mark,
		baseStop,
		risk,
		currentR,
		atr,
		regimeVol,
		assetClass,
	)
}

func currentRMultiple(side string, entry, mark, riskDistance float64) float64 {
	if side == "long" {
		return (mark - entry) / riskDistance
	}
	return (entry - mark) / riskDistance
}

func calculateDynamicStopLong(
	entry, mark, baseStop float64,
	risk *RiskSnapshot,
	currentR, atr, regimeVol float64,
	assetClass string,
) (float64, string, error) {
	if risk == nil {
		return 0, "", fmt.Errorf("风险信息缺失")
	}

	riskDistance := math.Abs(entry - risk.InitialStop)
	if riskDistance <= 0 {
		return 0, "", fmt.Errorf("风险距离无效")
	}

	lockRatio, baseATRMult, label := trailingParams(currentR, assetClass)
	atrMult := adjustATRMultiplier(baseATRMult, regimeVol, assetClass)

	lockedR := math.Max(currentR*lockRatio, 1)
	s1 := math.Max(entry+lockedR*riskDistance, entry)

	peak := risk.PeakPrice
	if peak <= 0 {
		peak = mark
	}
	s2 := peak - atr*atrMult

	candidate := math.Max(math.Min(s1, s2), baseStop)
	newStop := tightenStopLong(baseStop, candidate)
	suffix := ""
	if nearEqual(newStop, baseStop) {
		suffix = "（保持现有止损）"
	}

	reason := fmt.Sprintf(
		"%s：RegimeVol=%.4f，锁R=%.2fR，ATR(1H,14)=%.4f×%.2f → S1=%.4f，S2=%.4f，最终止损=%.4f%s",
		label, regimeVol, lockedR, atr, atrMult, s1, s2, newStop, suffix,
	)
	return newStop, reason, nil
}

func calculateDynamicStopShort(
	entry, mark, baseStop float64,
	risk *RiskSnapshot,
	currentR, atr, regimeVol float64,
	assetClass string,
) (float64, string, error) {
	if risk == nil {
		return 0, "", fmt.Errorf("风险信息缺失")
	}

	riskDistance := math.Abs(entry - risk.InitialStop)
	if riskDistance <= 0 {
		return 0, "", fmt.Errorf("风险距离无效")
	}

	lockRatio, baseATRMult, label := trailingParams(currentR, assetClass)
	atrMult := adjustATRMultiplier(baseATRMult, regimeVol, assetClass)

	lockedR := math.Max(currentR*lockRatio, 1)
	s1 := math.Min(entry-lockedR*riskDistance, entry)

	peak := risk.PeakPrice
	if peak <= 0 {
		peak = mark
	}
	s2 := peak + atr*atrMult

	candidate := math.Min(math.Max(s1, s2), baseStop)
	newStop := tightenStopShort(baseStop, candidate)
	suffix := ""
	if nearEqual(newStop, baseStop) {
		suffix = "（保持现有止损）"
	}

	reason := fmt.Sprintf(
		"%s：RegimeVol=%.4f，锁R=%.2fR，ATR(1H,14)=%.4f×%.2f → S1=%.4f，S2=%.4f，最终止损=%.4f%s",
		label, regimeVol, lockedR, atr, atrMult, s1, s2, newStop, suffix,
	)
	return newStop, reason, nil
}

// trailingParams 根据当前R决定锁定比例与基础ATR倍数
func trailingParams(currentR float64, assetClass string) (lockRatio float64, baseATRMult float64, label string) {
	switch assetClass {
	case "btc":
		switch {
		case currentR < 5:
			return 0.25, 3.0, "阶段2：BTC 趋势确认 (3-5R)"
		case currentR < 8:
			return 0.35, 2.7, "阶段3：BTC 吃中段 (5-8R)"
		default:
			return 0.40, 2.5, "阶段3：BTC 大波段 (8R+)"
		}
	default:
		switch {
		case currentR < 6:
			return 0.30, 3.2, "阶段2：热门币趋势确认 (3-6R)"
		case currentR < 10:
			return 0.40, 2.8, "阶段3：热门币中后段 (6-10R)"
		default:
			return 0.50, 2.4, "阶段3：热门币大波段 (10R+)"
		}
	}
}

// adjustATRMultiplier 根据 RegimeVol 调整 ATR 乘数
func adjustATRMultiplier(base float64, regimeVol float64, assetClass string) float64 {
	if regimeVol <= 0 {
		return base
	}

	switch assetClass {
	case "btc":
		switch {
		case regimeVol < 0.006:
			return base * 0.90
		case regimeVol > 0.012:
			return base * 1.15
		default:
			return base
		}
	default:
		switch {
		case regimeVol < 0.010:
			return base * 0.90
		case regimeVol > 0.020:
			return base * 1.25
		default:
			return base
		}
	}
}

// classifyAsset 将品种划分为 BTC 与热门趋势币两类
func classifyAsset(symbol string) string {
	s := strings.ToUpper(symbol)
	if strings.HasPrefix(s, "BTC") {
		return "btc"
	}
	return "trend_alt"
}

func tightenStopShort(current, candidate float64) float64 {
	if candidate < current {
		return candidate
	}
	return current
}

func tightenStopLong(current, candidate float64) float64 {
	if candidate > current {
		return candidate
	}
	return current
}

func nearEqual(a, b float64) bool {
	const epsilon = 1e-6
	return math.Abs(a-b) <= epsilon
}

func fetchOneHourATR(symbol string) (float64, error) {
	data, err := market.Get(symbol)
	if err != nil {
		return 0, fmt.Errorf("获取市场数据失败: %w", err)
	}
	if data == nil || len(data.Klines1h) == 0 {
		return 0, fmt.Errorf("1H ATR14 数据不可用")
	}

	atr := calculateATRFromKlines(data.Klines1h, atr1HPeriod)
	if atr <= 0 {
		return 0, fmt.Errorf("1H ATR14 数据不可用")
	}
	return atr, nil
}
