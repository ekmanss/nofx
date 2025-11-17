package trailingstop

import "strings"

// Config captures all tunable parameters that govern how the trailing stop logic behaves.
type Config struct {
	// ATRPeriod 用于计算ATR的周期（K线数量）。
	ATRPeriod int
	// PhaseStartBreakeven 触发保本阶段所需的最小R倍数。
	PhaseStartBreakeven float64
	// DefaultAssetClass 默认的资产分类（当无任何前缀规则匹配时使用）。
	DefaultAssetClass string
	// AssetClassRules 定义了Symbol前缀与资产分类之间的映射。
	AssetClassRules []AssetClassRule
	// AssetProfiles 为各资产分类提供分段参数与波动率调节配置。
	AssetProfiles map[string]*AssetProfile
}

// AssetClassRule associates a symbol prefix with an asset class key.
type AssetClassRule struct {
	// Prefix 用于匹配交易对前缀（不区分大小写）。
	Prefix string
	// Class 对应的资产分类标识符。
	Class string
}

// AssetProfile describes the trailing stop behavior for a single asset class.
type AssetProfile struct {
	// Ranges 描述了不同R区间的锁盈比例和ATR倍数。
	Ranges []TrailingRange
	// RegimeAdjustment 控制在不同波动率环境下ATR倍数的调整方式。
	RegimeAdjustment RegimeAdjustment
	// PeakDrawdownLimit 峰值回撤所允许的最大比例（例如0.12代表12%）。
	PeakDrawdownLimit float64
	// MaxRLockAlpha 峰值R需要锁定的比例，用于限制最大浮盈回吐。
	MaxRLockAlpha float64
}

// TrailingRange expresses how much R to lock and what ATR multiplier to use for a given band.
type TrailingRange struct {
	// MaxR 为该区间的上限R值；0表示无上限（最后一个区间）。
	MaxR float64
	// LockRatio 在该区间希望锁定的利润占当前R的比例。
	LockRatio float64
	// BaseATRMultiplier 基础ATR倍数，用于计算追踪止损与ATR之间的距离。
	BaseATRMultiplier float64
	// Label 用于日志输出的人类可读描述。
	Label string
}

// RegimeAdjustment defines how ATR multipliers react to volatility regimes.
type RegimeAdjustment struct {
	// LowThreshold 低波动率阈值，RegimeVol 低于此值时触发 LowMultiplier。
	LowThreshold float64
	// LowMultiplier 在低波动率环境下对 ATR 乘数的缩放比例。
	LowMultiplier float64
	// HighThreshold 高波动率阈值，RegimeVol 高于此值时触发 HighMultiplier。
	HighThreshold float64
	// HighMultiplier 在高波动率环境下对 ATR 乘数的放大比例。
	HighMultiplier float64
}

var defaultConfig = &Config{
	ATRPeriod:           14,
	PhaseStartBreakeven: 1.2,
	DefaultAssetClass:   "trend_alt",
	AssetClassRules: []AssetClassRule{
		{Prefix: "BTC", Class: "btc"},
	},
	AssetProfiles: map[string]*AssetProfile{
		"btc": {
			Ranges: []TrailingRange{
				{MaxR: 3, LockRatio: 0.35, BaseATRMultiplier: 2.7, Label: "阶段2：BTC 早期锁盈 (1.2-3R)"},
				{MaxR: 6, LockRatio: 0.50, BaseATRMultiplier: 2.3, Label: "阶段3：BTC 中段跟随 (3-6R)"},
				{MaxR: 0, LockRatio: 0.65, BaseATRMultiplier: 2.1, Label: "阶段4：BTC 大波段 (6R+)"},
			},
			RegimeAdjustment:  RegimeAdjustment{LowThreshold: 0.005, LowMultiplier: 0.90, HighThreshold: 0.012, HighMultiplier: 1.2},
			PeakDrawdownLimit: 0.12,
			MaxRLockAlpha:     0.60,
		},
		"trend_alt": {
			Ranges: []TrailingRange{
				{MaxR: 4, LockRatio: 0.40, BaseATRMultiplier: 3.1, Label: "阶段2：ALT 早期锁盈 (1.2-4R)"},
				{MaxR: 8, LockRatio: 0.55, BaseATRMultiplier: 2.6, Label: "阶段3：ALT 中段跟随 (4-8R)"},
				{MaxR: 0, LockRatio: 0.70, BaseATRMultiplier: 2.3, Label: "阶段4：ALT 大波段 (8R+)"},
			},
			RegimeAdjustment:  RegimeAdjustment{LowThreshold: 0.01, LowMultiplier: 0.90, HighThreshold: 0.050, HighMultiplier: 1.25},
			PeakDrawdownLimit: 0.15,
			MaxRLockAlpha:     0.45,
		},
	},
}

// DefaultConfig returns a deep copy of the built-in configuration so callers can tweak it safely.
func DefaultConfig() *Config {
	return defaultConfig.clone()
}

func resolveConfig(cfg *Config) *Config {
	base := defaultConfig.clone()
	if cfg == nil {
		return base
	}

	if cfg.ATRPeriod > 0 {
		base.ATRPeriod = cfg.ATRPeriod
	}
	if cfg.PhaseStartBreakeven > 0 {
		base.PhaseStartBreakeven = cfg.PhaseStartBreakeven
	}
	if cfg.DefaultAssetClass != "" {
		base.DefaultAssetClass = cfg.DefaultAssetClass
	}
	if len(cfg.AssetClassRules) > 0 {
		base.AssetClassRules = append([]AssetClassRule(nil), cfg.AssetClassRules...)
	}
	if len(cfg.AssetProfiles) > 0 {
		for k, profile := range cfg.AssetProfiles {
			if profile == nil {
				continue
			}
			base.AssetProfiles[k] = profile.clone()
		}
	}
	return base
}

func (c *Config) clone() *Config {
	if c == nil {
		return nil
	}
	clone := *c
	if len(c.AssetClassRules) > 0 {
		clone.AssetClassRules = append([]AssetClassRule(nil), c.AssetClassRules...)
	}
	clone.AssetProfiles = make(map[string]*AssetProfile, len(c.AssetProfiles))
	for key, profile := range c.AssetProfiles {
		clone.AssetProfiles[key] = profile.clone()
	}
	return &clone
}

func (p *AssetProfile) clone() *AssetProfile {
	if p == nil {
		return nil
	}
	clone := *p
	if len(p.Ranges) > 0 {
		clone.Ranges = append([]TrailingRange(nil), p.Ranges...)
	}
	return &clone
}

func (c *Config) assetClassForSymbol(symbol string) string {
	if c == nil {
		return ""
	}
	s := strings.ToUpper(strings.TrimSpace(symbol))
	for _, rule := range c.AssetClassRules {
		prefix := strings.ToUpper(rule.Prefix)
		if prefix != "" && strings.HasPrefix(s, prefix) {
			return rule.Class
		}
	}
	return c.DefaultAssetClass
}

func (c *Config) assetProfile(assetClass string) *AssetProfile {
	if c == nil {
		return nil
	}
	if profile, ok := c.AssetProfiles[assetClass]; ok && profile != nil {
		return profile
	}
	if c.DefaultAssetClass != "" {
		if profile, ok := c.AssetProfiles[c.DefaultAssetClass]; ok && profile != nil {
			return profile
		}
	}
	for _, profile := range c.AssetProfiles {
		if profile != nil {
			return profile
		}
	}
	return nil
}

func (c *Config) trailingParams(assetClass string, currentR float64) (float64, float64, string) {
	profile := c.assetProfile(assetClass)
	if profile == nil || len(profile.Ranges) == 0 {
		return 0.30, 3.0, "阶段2：默认"
	}
	for _, band := range profile.Ranges {
		if band.MaxR == 0 || currentR < band.MaxR {
			return band.LockRatio, band.BaseATRMultiplier, band.Label
		}
	}
	last := profile.Ranges[len(profile.Ranges)-1]
	return last.LockRatio, last.BaseATRMultiplier, last.Label
}

func (c *Config) adjustATRMultiplier(assetClass string, base, regimeVol float64) float64 {
	profile := c.assetProfile(assetClass)
	if profile == nil || regimeVol <= 0 {
		return base
	}
	adj := profile.RegimeAdjustment
	if adj.LowThreshold > 0 && adj.LowMultiplier > 0 && regimeVol < adj.LowThreshold {
		return base * adj.LowMultiplier
	}
	if adj.HighThreshold > 0 && adj.HighMultiplier > 0 && regimeVol > adj.HighThreshold {
		return base * adj.HighMultiplier
	}
	return base
}
