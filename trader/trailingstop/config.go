package trailingstop

import (
	"strings"
	"time"
)

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
	// DefaultMinLockedR 全局最小锁定R倍数（每个资产可以覆盖）。
	DefaultMinLockedR float64
	// TPlusTwoDuration T+2规则等待时间阈值。
	TPlusTwoDuration time.Duration
	// TPlusTwoLockRatio 达到T+2后需要锁定的峰值R比例。
	TPlusTwoLockRatio float64
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
	// MinLockedR 最小锁定R倍数（确保止损至少回到该R值）。
	MinLockedR float64
	// TPlusTwoDuration 该资产T+2规则的等待时间。
	TPlusTwoDuration time.Duration
	// TPlusTwoLockRatio T+2触发时锁定的峰值R比例。
	TPlusTwoLockRatio float64
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
	// 全局默认 ATR 周期缩短。
	// 既然你只看1H K线且持仓短，过去7根K线足够反应当前动能，14根太滞后。
	ATRPeriod: 7,

	// 全局保本触发：只要浮盈达到 0.8R，必须把风险敞口关掉。
	// 2%的本金风险很大，绝不能让一个已经跑出 0.8R 的单子最后变成亏损。
	PhaseStartBreakeven: 0.8,

	DefaultAssetClass: "trend_alt",
	DefaultMinLockedR: 0.2, // 最低也要锁住0.2R的利润（手续费保护）

	// T+2 规则修改：
	// 既然你预期持仓1-2小时，如果持仓超过 3小时 还没止盈，说明行情陷入僵局。
	// 这时候强制锁住大部分最高浮盈，准备离场。
	TPlusTwoDuration:  3 * time.Hour,
	TPlusTwoLockRatio: 0.80, // 时间到了没走出来，锁住80%的利润

	AssetClassRules: []AssetClassRule{
		{Prefix: "BTC", Class: "btc"},
		// 这里可以添加你那个热门山寨的前缀，例如 "SOL", "DOGE" 等
		// {Prefix: "SOL", Class: "trend_alt"},
	},

	AssetProfiles: map[string]*AssetProfile{
		// ==========================================
		// BTC 策略：稳健的一击脱离
		// ==========================================
		"btc": {
			ATRPeriod:           7,   // 7小时ATR，反应适中
			PhaseStartBreakeven: 0.6, // BTC波动小，跑出0.6R就开始保护本金
			MinLockedR:          0.2,

			// 既然是短线，我们不能容忍大利润回撤。
			// 允许最高浮盈回撤 30%，超过就走人。
			MaxRLockAlpha: 0.70,

			Ranges: []TrailingRange{
				// 【阶段1：启动期】 0 - 1.0R
				// 只要开始盈利，就用 2.5倍 ATR 保护。不求太紧，防止被杂波洗出去。
				{MaxR: 1.0, LockRatio: 0.2, BaseATRMultiplier: 2.5, Label: "🛡️ BTC启动保护"},

				// 【阶段2：达标期】 1.0R - 2.0R
				// 你的目标大概率落在这个区间。
				// 一旦超过1R，ATR系数收紧到 1.5。我们不贪大趋势，只要这波惯性。
				{MaxR: 2.0, LockRatio: 0.5, BaseATRMultiplier: 1.5, Label: "💰 BTC达标锁利"},

				// 【阶段3：超预期】 > 2.0R
				// 如果运气好遇到了大爆发，紧贴价格（1.0 ATR）。
				{MaxR: 0, LockRatio: 0.8, BaseATRMultiplier: 1.0, Label: "🚀 BTC加速冲顶"},
			},

			// BTC 波动率修正
			// 低波动时（横盘）稍微敏感点，高波动时（插针）稍微宽一点防扫损
			RegimeAdjustment: RegimeAdjustment{LowThreshold: 0.005, LowMultiplier: 0.9, HighThreshold: 0.025, HighMultiplier: 1.2},
		},

		// ==========================================
		// 热门山寨策略：高波动，快进快出
		// ==========================================
		"trend_alt": {
			ATRPeriod:           5,   // 山寨币变脸极快，只看过去5小时
			PhaseStartBreakeven: 0.8, // 山寨波动大，给它0.8R的呼吸空间再保本
			MinLockedR:          0.2,

			// 山寨币波动剧烈，允许回撤 40% 的利润（比BTC宽），否则很容易被震下车。
			MaxRLockAlpha: 0.60,

			Ranges: []TrailingRange{
				// 【阶段1：噪音过滤】 0 - 1.5R
				// 山寨币经常来回扫。在赚到 1.5R 之前，我们给 3.0 ATR 的宽容度。
				// 只要没打到开仓价，就让它折腾。
				{MaxR: 1.5, LockRatio: 0.1, BaseATRMultiplier: 3.0, Label: "🎢 山寨抗震荡"},

				// 【阶段2：主升浪】 1.5R - 3.0R
				// 既然是热门币，爆发力强。一旦冲过1.5R，必须开始重仓锁利。
				// 系数降到 2.0。
				{MaxR: 3.0, LockRatio: 0.6, BaseATRMultiplier: 2.0, Label: "📈 山寨主升浪"},

				// 【阶段3：疯牛】 > 3.0R
				// 这种单子在1-2小时内很难遇到，如果遇到了，绝对是运气。
				// 极度收紧，防止瀑布。
				{MaxR: 0, LockRatio: 0.9, BaseATRMultiplier: 1.2, Label: "💸 山寨极速落袋"},
			},

			// 山寨币波动率极高时的特殊处理
			// 如果波动率突然爆表（RegimeVol > 0.10，即10%），ATR倍数放大1.3倍
			// 防止被做市商的“天地针”定点爆破
			RegimeAdjustment: RegimeAdjustment{LowThreshold: 0.02, LowMultiplier: 1.0, HighThreshold: 0.10, HighMultiplier: 1.3},
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
	if cfg.DefaultMinLockedR > 0 {
		base.DefaultMinLockedR = cfg.DefaultMinLockedR
	}
	if cfg.TPlusTwoDuration > 0 {
		base.TPlusTwoDuration = cfg.TPlusTwoDuration
	}
	if cfg.TPlusTwoLockRatio > 0 {
		base.TPlusTwoLockRatio = cfg.TPlusTwoLockRatio
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

func (c *Config) minLockedRForClass(assetClass string) float64 {
	if profile := c.assetProfile(assetClass); profile != nil && profile.MinLockedR > 0 {
		return profile.MinLockedR
	}
	if c != nil && c.DefaultMinLockedR > 0 {
		return c.DefaultMinLockedR
	}
	if defaultConfig != nil && defaultConfig.DefaultMinLockedR > 0 {
		return defaultConfig.DefaultMinLockedR
	}
	return 0
}

func (c *Config) tPlusTwoLockRatioForClass(assetClass string) float64 {
	if profile := c.assetProfile(assetClass); profile != nil && profile.TPlusTwoLockRatio > 0 {
		return profile.TPlusTwoLockRatio
	}
	if c != nil && c.TPlusTwoLockRatio > 0 {
		return c.TPlusTwoLockRatio
	}
	if defaultConfig != nil && defaultConfig.TPlusTwoLockRatio > 0 {
		return defaultConfig.TPlusTwoLockRatio
	}
	return 0
}

func (c *Config) tPlusTwoDurationForClass(assetClass string) time.Duration {
	if profile := c.assetProfile(assetClass); profile != nil && profile.TPlusTwoDuration > 0 {
		return profile.TPlusTwoDuration
	}
	if c != nil && c.TPlusTwoDuration > 0 {
		return c.TPlusTwoDuration
	}
	if defaultConfig != nil && defaultConfig.TPlusTwoDuration > 0 {
		return defaultConfig.TPlusTwoDuration
	}
	return 0
}
