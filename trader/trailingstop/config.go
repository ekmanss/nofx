package trailingstop

import (
	"strings"
	"time"
)

// Config captures all tunable parameters that govern how the trailing stop logic behaves.
type Config struct {
	// ATRPeriod 用于计算ATR的周期（K线数量）。
	ATRPeriod int
	// ATRInterval ATR 数据使用的 K 线周期（如 "1h"、"4h"、"1d"）。
	ATRInterval string
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
	// ATRInterval 为该资产分类单独配置ATR K线周期（非空时覆盖全局）。
	ATRInterval string
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
	// 全局默认 ATR 周期：
	ATRPeriod: 5,
	// 默认使用 1H K 线计算 ATR，可根据策略改为 4H/1D。
	ATRInterval: "4h",

	// 全局保本触发：只要浮盈达到 0.8R，必须把风险敞口关掉。
	// 2% 的本金风险很大，绝不能让一个已经跑出 0.8R 的单子最后变成亏损。
	PhaseStartBreakeven: 0.8,

	// 默认资产类型（当前缀规则都没命中时，就按山寨策略处理）
	DefaultAssetClass: "trend_alt",

	// 默认最小锁定 R：至少锁 0.2R（覆盖手续费 + 一点利润）
	DefaultMinLockedR: 0.2,

	// T+2 规则：
	// 预期持仓 1–2 小时，如果超过 3 小时还没结束，说明行情拖沓。
	// 这时候强制锁住大部分最高浮盈，准备离场。
	TPlusTwoDuration:  2 * time.Hour,
	TPlusTwoLockRatio: 0.8, // 全局默认锁峰值 R 的 80%，具体资产可以覆盖

	// 简单的资产分类规则
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
			// 4H K线 ATR 周期
			ATRPeriod:   5, // 计算过去 20 小时的波动率
			ATRInterval: "4h",

			// 【核心修改 1：极大放宽保本触发线】
			// BTC 经常在浮盈 0.6R-0.8R 处回撤。
			// 我们硬气一点，不到 1R (约 1.5% 涨跌幅) 坚决不移动止损。
			// 宁可被打止损，也不能被噪音洗下车，错失后面的 3R。
			PhaseStartBreakeven: 1.0,

			// 【核心修改 2：保本只保手续费】
			// 触发保本后，只锁 0.1R。不要锁太多，给价格回踩开仓价留出呼吸空间。
			MinLockedR: 0.1,

			// 限制最大回撤锁定比例，防止在 R 值很高时回吐太多，但也不要锁太死
			MaxRLockAlpha: 0.60,

			Ranges: []TrailingRange{
				// 【阶段 1：静默期】 0R - 1.2R
				// 策略：装死。
				// 只要价格没冲过 1.2R，就用非常宽的 ATR (3.5倍) 或者干脆不 trailing。
				// 我们赌的就是它能突破，如果不能，就止损认赔，不搞微操。
				{MaxR: 1.2, LockRatio: 0.05, BaseATRMultiplier: 3.5, Label: "🧘 BTC 波动容忍区"},

				// 【阶段 2：趋势确立期】 1.2R - 2.8R
				// 策略：跟随。
				// 利润已经打出来了，开始把止损上移。BaseATRMultiplier 2.0 是 BTC 4h 趋势的黄金均线距离。
				{MaxR: 2.8, LockRatio: 0.5, BaseATRMultiplier: 2.0, Label: "📈 BTC 趋势跟随"},

				// 【阶段 3：止盈收割期】 > 2.8R
				// 策略：收网。
				// 既然已经到了你的 3R 目标区，可以激进一点锁利润了。
				{MaxR: 0, LockRatio: 0.8, BaseATRMultiplier: 1.2, Label: "💰 BTC 止盈收割"},
			},

			// 波动率自适应：BTC 波动率低时（横盘），ATR 止损要收紧，防止阴跌。
			RegimeAdjustment: RegimeAdjustment{
				LowThreshold:   0.008, // 4h 波动率低于 0.8%
				LowMultiplier:  0.8,   // 止损收紧
				HighThreshold:  0.04,  // 4h 波动率高于 4% (大暴涨/暴跌)
				HighMultiplier: 1.5,   // 止损放宽，防插针
			},

			// 【核心修改 3：给足时间】
			// 4h 级别的趋势，24 小时（6 根 K 线）是最小的检验周期。
			// 如果 24 小时后 R 还没跑出来，说明趋势大概率没了，这时候才强制离场。
			TPlusTwoDuration:  24 * time.Hour,
			TPlusTwoLockRatio: 0.9,
		},

		// ==========================================
		// 热门山寨策略：高波动，快进快出
		// ==========================================
		"trend_alt": {
			ATRPeriod:           5,    // 山寨变脸极快，只看过去 5 小时
			ATRInterval:         "1h", // 使用 1H K 线
			PhaseStartBreakeven: 1.0,  // ✅ 调高：至少跑出 1R 再保本，减少无谓来回扫
			MinLockedR:          0.2,

			// 山寨允许更大的利润回撤，否则极易在震荡里被挤出局。
			MaxRLockAlpha: 0.60,

			// ✅ 单独覆盖 T+2：同样是 3 小时，但只锁 60% 峰值 R，
			// 给山寨在后半段再冲一波的空间。
			TPlusTwoDuration:  2 * time.Hour,
			TPlusTwoLockRatio: 0.6,

			Ranges: []TrailingRange{
				// 【阶段 1：噪音过滤】 0 - 1.5R
				// 山寨经常来回扫，在 1.5R 之前给 3 ATR 的宽容度。
				// 锁 0.15R，保护一点利润，但重点是“别太早锁死”。
				{MaxR: 1.5, LockRatio: 0.15, BaseATRMultiplier: 3.0, Label: "🎢 山寨抗震荡"},

				// 【阶段 2：主升浪】 1.5R - 3.0R
				// 热门币爆发力强，冲过 1.5R 后开始重仓锁利。
				// ✅ ATR 略收紧到 1.8，提升平均实现 R，但不过度紧缩。
				{MaxR: 3.0, LockRatio: 0.6, BaseATRMultiplier: 1.8, Label: "📈 山寨主升浪"},

				// 【阶段 3：疯牛】 > 3.0R
				// 1–2 小时里比较少见，一旦出现，就是运气。
				// 极度收紧防止瀑布，优先稳稳落袋为主。
				{MaxR: 0, LockRatio: 0.9, BaseATRMultiplier: 1.2, Label: "💸 山寨极速落袋"},
			},

			// 山寨币波动率极高时的特殊处理：
			// RegimeVol > 10% 时放宽 ATR 倍数 1.3 倍，防止“天地针”定点爆破。
			RegimeAdjustment: RegimeAdjustment{
				LowThreshold:   0.02,
				LowMultiplier:  1.0,
				HighThreshold:  0.10,
				HighMultiplier: 1.3,
			},
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
	if interval := normalizeATRInterval(cfg.ATRInterval); interval != "" {
		base.ATRInterval = interval
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

func (c *Config) atrIntervalForClass(assetClass string) string {
	if c == nil {
		return normalizeATRInterval(defaultConfig.ATRInterval)
	}
	if profile := c.assetProfile(assetClass); profile != nil {
		if interval := normalizeATRInterval(profile.ATRInterval); interval != "" {
			return interval
		}
	}
	if interval := normalizeATRInterval(c.ATRInterval); interval != "" {
		return interval
	}
	if defaultConfig != nil {
		return normalizeATRInterval(defaultConfig.ATRInterval)
	}
	return ""
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

func normalizeATRInterval(interval string) string {
	switch strings.ToLower(strings.TrimSpace(interval)) {
	case "1h", "4h", "1d":
		return strings.ToLower(interval)
	default:
		return ""
	}
}
