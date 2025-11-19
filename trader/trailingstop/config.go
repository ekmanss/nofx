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
	// ATRPeriod 为该资产分类单独配置ATR计算周期（>0时生效）。
	ATRPeriod int
	// MaxRLockAlpha 峰值R需要锁定的比例，用于限制最大浮盈回吐。
	MaxRLockAlpha float64
	// PhaseStartBreakeven 触发保本阶段所需的最小R倍数（>0时覆盖全局配置）。
	PhaseStartBreakeven float64
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
	PhaseStartBreakeven: 1.0,
	DefaultAssetClass:   "trend_alt",
	AssetClassRules: []AssetClassRule{
		{Prefix: "BTC", Class: "btc"},
	},
	AssetProfiles: map[string]*AssetProfile{
		"btc": {
			// 1H级别，BTC不需要像山寨那样看7根K线，14根（默认）稍微滞后，建议改为10
			ATRPeriod: 10,

			Ranges: []TrailingRange{
				// 【阶段1：生存期】 0 - 1.2R
				// 只要浮盈超过 1R，立刻把止损提到入场价上方一点点（LockRatio 0.1）。
				// ATR倍数给 3.0，容忍 BTC 的初期磨蹭和假突破。
				{MaxR: 1.2, LockRatio: 0.10, BaseATRMultiplier: 3.0, Label: "🛡️ 阶段1：BTC 成本保护"},

				// 【阶段2：利润收割期】 1.2R - 2.5R
				// 这是你 1-2 小时持仓最容易达到的区间。
				// 必须激进锁利！到达 2.5R 时，至少要锁住 60% 的利润。
				// ATR 降为 2.0，贴紧价格走。
				{MaxR: 2.5, LockRatio: 0.60, BaseATRMultiplier: 2.0, Label: "💰 阶段2：BTC 主升浪锁盈"},

				// 【阶段3：意外之喜】 > 2.5R
				// 如果 2小时内 BTC 拉了超过 2.5R，说明遇到大事件了。
				// 这种行情通常不可持续，用极紧的 1.5 ATR 跟踪，稍有风吹草动就离场。
				{MaxR: 0, LockRatio: 0.80, BaseATRMultiplier: 1.5, Label: "🚀 阶段3：BTC 加速冲顶"},
			},

			// 【波动率适应】
			// BTC 低波动时（横盘震荡）往往是在蓄势，不要随意收紧止损，保持 1.0。
			// 高波动时（插针乱飞），稍微放大 ATR 倍数（1.1），防止被“天地针”扫地出门。
			RegimeAdjustment: RegimeAdjustment{LowThreshold: 0.005, LowMultiplier: 1.0, HighThreshold: 0.020, HighMultiplier: 1.1},

			// 【回撤限制】
			// 限制最大回吐 R 值，只允许回吐 40% 的 R
			MaxRLockAlpha: 0.60,
		},
		"trend_alt": {
			ATRPeriod:           7,   // 保持7，反应快是好事
			PhaseStartBreakeven: 0.8, // 山寨币更早启动追踪（0.8R就开始），避免回吐太多利润
			Ranges: []TrailingRange{
				// 阶段1：快速保本。只要赚了1.5R，立刻把止损提到入场价上方（锁0.1R），防止白玩。
				{MaxR: 1.5, LockRatio: 0.1, BaseATRMultiplier: 3.5, Label: "⚡️ 阶段1：快速保本"},
				// 阶段2：主要利润段。赚到3R时，必须锁住一半利润。ATR系数收紧到 2.5。
				{MaxR: 3.0, LockRatio: 0.50, BaseATRMultiplier: 2.5, Label: "📈 阶段2：锁定半程"},
				// 阶段3：加速段。如果是"疯牛"行情，赚到5R以上，紧紧贴着价格走，ATR降到 1.8。
				{MaxR: 5.0, LockRatio: 0.70, BaseATRMultiplier: 1.8, Label: "🚀 阶段3：加速冲刺"},
				// 阶段4：极值。防止大瀑布。
				{MaxR: 0, LockRatio: 0.85, BaseATRMultiplier: 1.5, Label: "💰 阶段4：落袋为安"},
			},
			// 波动率调整：对于热门币，波动率低时反而要敏感（0.8），波动率极大时适当放宽（1.2）防止被插针洗盘
			RegimeAdjustment: RegimeAdjustment{LowThreshold: 0.02, LowMultiplier: 0.8, HighThreshold: 0.08, HighMultiplier: 1.2},
			MaxRLockAlpha:    0.60,
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

func (c *Config) atrPeriodForClass(assetClass string) int {
	if c == nil {
		return 0
	}
	if profile := c.assetProfile(assetClass); profile != nil && profile.ATRPeriod > 0 {
		return profile.ATRPeriod
	}
	return c.ATRPeriod
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

func (c *Config) phaseStartBreakevenForClass(assetClass string) float64 {
	if c == nil {
		return 0
	}
	if profile := c.assetProfile(assetClass); profile != nil && profile.PhaseStartBreakeven > 0 {
		return profile.PhaseStartBreakeven
	}
	return c.PhaseStartBreakeven
}
