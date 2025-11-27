package trailingstop

import (
	"fmt"
	"math"
	"nofx/market"
	"strings"
	"time"
)

// RiskSnapshot is a lightweight view of the information needed to compute the trailing stop.
type RiskSnapshot struct {
	InitialStop float64
	PeakPrice   float64
	MaxR        float64
	OpenedAt    time.Time
}

// ATRFetcher allows tests to provide deterministic ATR data.
type ATRFetcher func(symbol, interval string, period int) (float64, error)

// ATRTrailingCalculator encapsulates the ATR-based trailing stop rules.
type ATRTrailingCalculator struct {
	fetchATR ATRFetcher
	config   *Config
}

// NewATRTrailingCalculator creates a calculator using the default trailing-stop configuration.
func NewATRTrailingCalculator(fetcher ATRFetcher) *ATRTrailingCalculator {
	return NewATRTrailingCalculatorWithConfig(fetcher, nil)
}

// NewATRTrailingCalculatorWithConfig allows callers to customize both the ATR fetcher and
// the trailing-stop configuration.
func NewATRTrailingCalculatorWithConfig(fetcher ATRFetcher, cfg *Config) *ATRTrailingCalculator {
	resolved := resolveConfig(cfg)
	if fetcher == nil {
		fetcher = fetchATRWithInterval
	}
	return &ATRTrailingCalculator{fetchATR: fetcher, config: resolved}
}

// Calculate returns the next stop price together with a human readable explanation.
func (c *ATRTrailingCalculator) Calculate(
	pos *Snapshot,
	risk *RiskSnapshot,
	prevStop float64,
	hasPrevStop bool,
) (float64, bool, string, error) {
	if c == nil || c.config == nil {
		return 0, false, "", fmt.Errorf("止损配置缺失")
	}
	if pos == nil {
		return 0, false, "", fmt.Errorf("持仓信息缺失")
	}
	if risk == nil {
		return 0, false, "", fmt.Errorf("风险信息缺失")
	}

	entry := pos.EntryPrice
	mark := pos.MarkPrice
	riskDistance := math.Abs(entry - risk.InitialStop)
	if riskDistance <= 0 {
		return 0, false, "", fmt.Errorf("风险距离无效")
	}

	currentR := currentRMultiple(pos.Side, entry, mark, riskDistance)
	baseStop := risk.InitialStop
	if hasPrevStop {
		baseStop = prevStop
	}

	assetClass := c.config.assetClassForSymbol(pos.Symbol)
	phaseStartBreakeven := c.config.phaseStartBreakevenForClass(assetClass)
	if currentR < phaseStartBreakeven {
		// 阶段0 也允许 T+2：持仓拖延太久时，直接按峰值R强制锁利
		stageOneMax := stageOneMaxR(c.config.assetProfile(assetClass))
		tPlusTwoLockRatio := c.config.tPlusTwoLockRatioForClass(assetClass)
		tPlusTwoDuration := c.config.tPlusTwoDurationForClass(assetClass)
		var (
			tPlusTwoStop    float64
			tPlusTwoApplied bool
		)
		if pos.Side == "long" {
			tPlusTwoStop, tPlusTwoApplied = applyTPlusTwoLong(risk, stageOneMax, currentR, entry, riskDistance, tPlusTwoLockRatio, tPlusTwoDuration)
		} else {
			tPlusTwoStop, tPlusTwoApplied = applyTPlusTwoShort(risk, stageOneMax, currentR, entry, riskDistance, tPlusTwoLockRatio, tPlusTwoDuration)
		}

		if tPlusTwoApplied {
			forceExit := (pos.Side == "long" && tPlusTwoStop >= mark) || (pos.Side == "short" && tPlusTwoStop <= mark)
			var newStop float64
			if pos.Side == "long" {
				newStop = tightenStopLong(baseStop, tPlusTwoStop)
			} else {
				newStop = tightenStopShort(baseStop, tPlusTwoStop)
			}

			suffix := ""
			if floatsAlmostEqual(newStop, baseStop) {
				suffix = "（保持现有止损）"
			}

			reason := fmt.Sprintf("阶段0：<%.2fR，T+2=%.4f，最终止损=%.4f%s", phaseStartBreakeven, tPlusTwoStop, newStop, suffix)
			if forceExit {
				reason += "（触发强制平仓）"
			}
			return newStop, forceExit, reason, nil
		}

		return baseStop, false, fmt.Sprintf("阶段0：<%.2fR，保持止损 %.4f", phaseStartBreakeven, baseStop), nil
	}

	atrPeriod := c.config.atrPeriodForClass(assetClass)

	atrInterval := c.config.atrIntervalForClass(assetClass)
	if atrInterval == "" {
		atrInterval = "1h"
	}

	atr, err := c.fetchATR(pos.Symbol, atrInterval, atrPeriod)
	if err != nil {
		return 0, false, "", err
	}
	if atr <= 0 {
		return 0, false, "", fmt.Errorf("%s ATR%d 数据不可用", strings.ToUpper(atrInterval), atrPeriod)
	}

	regimeVol := atr / mark

	if pos.Side == "long" {
		return calculateDynamicStopLong(
			entry,
			mark,
			baseStop,
			risk,
			currentR,
			atr,
			regimeVol,
			atrPeriod,
			atrInterval,
			assetClass,
			c.config,
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
		atrPeriod,
		atrInterval,
		assetClass,
		c.config,
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
	atrPeriod int,
	atrInterval string,
	assetClass string,
	cfg *Config,
) (float64, bool, string, error) {
	if risk == nil {
		return 0, false, "", fmt.Errorf("风险信息缺失")
	}

	riskDistance := math.Abs(entry - risk.InitialStop)
	if riskDistance <= 0 {
		return 0, false, "", fmt.Errorf("风险距离无效")
	}

	profile := cfg.assetProfile(assetClass)
	lockRatio, baseATRMult, label := cfg.trailingParams(assetClass, currentR)
	atrMult := cfg.adjustATRMultiplier(assetClass, baseATRMult, regimeVol)

	minLockedR := cfg.minLockedRForClass(assetClass)
	lockedR := math.Max(currentR*lockRatio, minLockedR)
	var alphaLock float64
	if profile != nil && profile.MaxRLockAlpha > 0 && risk.MaxR > 0 {
		alphaLock = risk.MaxR * profile.MaxRLockAlpha
		if alphaLock > currentR {
			alphaLock = currentR
		}
		if alphaLock > lockedR {
			lockedR = alphaLock
		}
	}

	s1 := math.Max(entry+lockedR*riskDistance, entry)

	peak := risk.PeakPrice
	if peak <= 0 {
		peak = mark
	}
	s2 := peak - atr*atrMult

	candidate := math.Max(baseStop, math.Max(s1, s2))
	forceExit := false

	newStop := tightenStopLong(baseStop, candidate)
	suffix := ""
	if floatsAlmostEqual(newStop, baseStop) {
		suffix = "（保持现有止损）"
	}

	intervalLabel := strings.ToUpper(atrInterval)
	reason := fmt.Sprintf(
		"%s：RegimeVol=%.4f，锁R=%.2fR（MaxR=%.2fR，Alpha=%.2fR），ATR(%s,%d)=%.4f×%.2f → S1=%.4f，S2=%.4f，最终止损=%.4f%s",
		label, regimeVol, lockedR, risk.MaxR, alphaLock, intervalLabel, atrPeriod, atr, atrMult, s1, s2, newStop, suffix,
	)
	return newStop, forceExit, reason, nil
}

func calculateDynamicStopShort(
	entry, mark, baseStop float64,
	risk *RiskSnapshot,
	currentR, atr, regimeVol float64,
	atrPeriod int,
	atrInterval string,
	assetClass string,
	cfg *Config,
) (float64, bool, string, error) {
	if risk == nil {
		return 0, false, "", fmt.Errorf("风险信息缺失")
	}

	riskDistance := math.Abs(entry - risk.InitialStop)
	if riskDistance <= 0 {
		return 0, false, "", fmt.Errorf("风险距离无效")
	}

	profile := cfg.assetProfile(assetClass)
	lockRatio, baseATRMult, label := cfg.trailingParams(assetClass, currentR)
	atrMult := cfg.adjustATRMultiplier(assetClass, baseATRMult, regimeVol)

	minLockedR := cfg.minLockedRForClass(assetClass)
	lockedR := math.Max(currentR*lockRatio, minLockedR)
	var alphaLock float64
	if profile != nil && profile.MaxRLockAlpha > 0 && risk.MaxR > 0 {
		alphaLock = risk.MaxR * profile.MaxRLockAlpha
		if alphaLock > currentR {
			alphaLock = currentR
		}
		if alphaLock > lockedR {
			lockedR = alphaLock
		}
	}

	s1 := math.Min(entry-lockedR*riskDistance, entry)

	peak := risk.PeakPrice
	if peak <= 0 {
		peak = mark
	}
	s2 := peak + atr*atrMult

	candidate := math.Min(baseStop, math.Min(s1, s2))
	forceExit := false

	newStop := tightenStopShort(baseStop, candidate)
	suffix := ""
	if floatsAlmostEqual(newStop, baseStop) {
		suffix = "（保持现有止损）"
	}

	intervalLabel := strings.ToUpper(atrInterval)
	reason := fmt.Sprintf(
		"%s：RegimeVol=%.4f，锁R=%.2fR（MaxR=%.2fR，Alpha=%.2fR），ATR(%s,%d)=%.4f×%.2f → S1=%.4f，S2=%.4f，最终止损=%.4f%s",
		label, regimeVol, lockedR, risk.MaxR, alphaLock, intervalLabel, atrPeriod, atr, atrMult, s1, s2, newStop, suffix,
	)
	return newStop, forceExit, reason, nil
}

func stageOneMaxR(profile *AssetProfile) float64 {
	if profile == nil || len(profile.Ranges) == 0 {
		return 0
	}
	maxR := profile.Ranges[0].MaxR
	if maxR <= 0 {
		return 0
	}
	return maxR
}

func applyTPlusTwoLong(risk *RiskSnapshot, stageOneMax, currentR, entry, riskDistance float64, lockRatio float64, duration time.Duration) (float64, bool) {
	if lockRatio <= 0 || duration <= 0 {
		return 0, false
	}
	if !shouldApplyTPlusTwo(risk, stageOneMax, currentR, duration) {
		return 0, false
	}
	targetR := risk.MaxR * lockRatio
	if targetR < 0 {
		return entry, true
	}
	stop := entry + targetR*riskDistance
	if stop < entry {
		stop = entry
	}
	return stop, true
}

func applyTPlusTwoShort(risk *RiskSnapshot, stageOneMax, currentR, entry, riskDistance float64, lockRatio float64, duration time.Duration) (float64, bool) {
	if lockRatio <= 0 || duration <= 0 {
		return 0, false
	}
	if !shouldApplyTPlusTwo(risk, stageOneMax, currentR, duration) {
		return 0, false
	}
	targetR := risk.MaxR * lockRatio
	if targetR < 0 {
		return entry, true
	}
	stop := entry - targetR*riskDistance
	if stop > entry {
		stop = entry
	}
	return stop, true
}

func shouldApplyTPlusTwo(risk *RiskSnapshot, stageOneMax, currentR float64, duration time.Duration) bool {
	if risk == nil {
		return false
	}
	if risk.OpenedAt.IsZero() {
		return false
	}
	if duration <= 0 {
		return false
	}
	if stageOneMax <= 0 {
		return false
	}
	if risk.MaxR <= 0 {
		return false
	}
	if currentR <= 0 {
		return false
	}
	if currentR >= stageOneMax {
		return false
	}
	if time.Since(risk.OpenedAt) < duration {
		return false
	}
	return true
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

func fetchATRWithInterval(symbol, interval string, period int) (float64, error) {
	apiClient := market.NewAPIClient()
	normalized := market.Normalize(symbol)

	interval = strings.ToLower(strings.TrimSpace(interval))
	if interval == "" {
		interval = "1h"
	}

	limit := period * 2
	if limit < period+1 {
		limit = period + 1
	}

	klines, err := apiClient.GetKlines(normalized, interval, limit)
	if err != nil {
		return 0, fmt.Errorf("获取 %s K线失败: %w", strings.ToUpper(interval), err)
	}
	if len(klines) <= period {
		return 0, fmt.Errorf("%s ATR%d 数据不足", strings.ToUpper(interval), period)
	}

	atr := calculateATRFromKlines(klines, period)
	if atr <= 0 {
		return 0, fmt.Errorf("%s ATR%d 数据不可用", strings.ToUpper(interval), period)
	}
	return atr, nil
}
