package trader

import (
	"fmt"
	"log"
	"math"
	"nofx/decision"
	"nofx/logger"
	"nofx/market"
	"strconv"
	"strings"
	"sync"
	"time"
)

type sharedTrailingStopEntry struct {
	monitor *TrailingStopMonitor
	owners  map[string]*AutoTrader
}

// SharedTrailingStopMonitor 为共享账户提供引用计数包装
type SharedTrailingStopMonitor struct {
	accountKey string
	traderID   string
	entry      *sharedTrailingStopEntry
}

var (
	sharedTrailingStopMu sync.Mutex
	sharedTrailingStops  = make(map[string]*sharedTrailingStopEntry)
)

// AcquireSharedTrailingStopMonitor 获取/创建共享的追踪止损监控器
func AcquireSharedTrailingStopMonitor(at *AutoTrader) *SharedTrailingStopMonitor {
	if at == nil {
		return nil
	}

	if at.accountKey == "" {
		at.accountKey = generateAccountKey(at.config)
	}

	sharedTrailingStopMu.Lock()
	defer sharedTrailingStopMu.Unlock()

	entry, exists := sharedTrailingStops[at.accountKey]
	if !exists {
		entry = &sharedTrailingStopEntry{
			monitor: NewTrailingStopMonitor(at),
			owners:  make(map[string]*AutoTrader),
		}
		sharedTrailingStops[at.accountKey] = entry
		log.Printf("🆕 [追踪止损] 创建账户监控器: %s (首个交易员: %s)", maskAccountKey(at.accountKey), at.name)
	} else {
		log.Printf("♻️ [追踪止损] 复用账户监控器: %s (新增交易员: %s)", maskAccountKey(at.accountKey), at.name)
	}

	entry.owners[at.id] = at
	entry.monitor.SetOwner(at)

	return &SharedTrailingStopMonitor{
		accountKey: at.accountKey,
		traderID:   at.id,
		entry:      entry,
	}
}

func maskAccountKey(key string) string {
	if len(key) <= 8 {
		return key
	}
	return fmt.Sprintf("%s...%s", key[:4], key[len(key)-4:])
}

// Start 启动共享监控器
func (m *SharedTrailingStopMonitor) Start() {
	if m == nil || m.entry == nil {
		return
	}
	m.entry.monitor.Start()
}

// Stop 释放共享监控器引用
func (m *SharedTrailingStopMonitor) Stop() {
	if m == nil || m.entry == nil {
		return
	}

	var (
		monitorToStop *TrailingStopMonitor
		nextOwner     *AutoTrader
		remaining     int
	)

	sharedTrailingStopMu.Lock()
	if entry, exists := sharedTrailingStops[m.accountKey]; exists && entry == m.entry {
		delete(entry.owners, m.traderID)
		remaining = len(entry.owners)
		if remaining == 0 {
			delete(sharedTrailingStops, m.accountKey)
			monitorToStop = entry.monitor
		} else {
			for _, candidate := range entry.owners {
				nextOwner = candidate
				break
			}
		}
	}
	sharedTrailingStopMu.Unlock()

	if monitorToStop != nil {
		monitorToStop.Stop()
		log.Printf("🛑 [追踪止损] 关闭账户监控器: %s（无活跃交易员）", maskAccountKey(m.accountKey))
	} else if nextOwner != nil {
		m.entry.monitor.SetOwner(nextOwner)
		log.Printf("👑 [追踪止损] 切换监控器负责人 → %s (账户: %s)", nextOwner.name, maskAccountKey(m.accountKey))
	}

	m.entry = nil
}

// ClearPosition 透传到真实监控器
func (m *SharedTrailingStopMonitor) ClearPosition(symbol, side string) {
	if m == nil || m.entry == nil {
		return
	}
	m.entry.monitor.ClearPosition(symbol, side)
}

// TrailingStopMonitor 动态追踪止损监控器
// 功能：当持仓收益>2%时，自动设置动态止损，从最高价回撤40%时触发
type TrailingStopMonitor struct {
	trader               *AutoTrader
	historicalPeakPrices map[string]float64 // symbol_side -> 历史最高/最低价格
	lastStopLossPrices   map[string]float64 // symbol_side -> 上次设置的止损价（避免重复调用API）
	mu                   sync.RWMutex
	stopCh               chan struct{} // 用于停止监控goroutine
	wg                   sync.WaitGroup
	isRunning            bool
}

const (
	trailingCheckInterval = 5 * time.Second
	minProfitThresholdPct = 5.0
	mediumProfitUpperPct  = 10.0
	mediumDrawdownPct     = 0.50
	highDrawdownPct       = 0.35
	defaultLeverage       = 5
)

type positionSnapshot struct {
	Symbol     string
	Side       string
	EntryPrice float64
	MarkPrice  float64
	Quantity   float64
	Leverage   int
}

func (p positionSnapshot) profitPct() float64 {
	if p.EntryPrice == 0 {
		return 0
	}
	priceMove := (p.MarkPrice - p.EntryPrice) / p.EntryPrice
	if p.Side == "short" {
		priceMove = -priceMove
	}
	return priceMove * float64(p.Leverage) * 100
}

func (p positionSnapshot) key() string {
	return p.Symbol + "_" + p.Side
}

// determineTrailingPercents 根据收益率返回允许的回撤比例和保留收益比例
func determineTrailingPercents(profitPct float64) (drawdownPct, retainPct float64) {
	if profitPct < minProfitThresholdPct {
		return 0, 0
	}
	if profitPct <= mediumProfitUpperPct {
		return mediumDrawdownPct, 1.0 - mediumDrawdownPct
	}
	return highDrawdownPct, 1.0 - highDrawdownPct
}

// NewTrailingStopMonitor 创建动态止损监控器
func NewTrailingStopMonitor(trader *AutoTrader) *TrailingStopMonitor {
	return &TrailingStopMonitor{
		trader:               trader,
		historicalPeakPrices: make(map[string]float64),
		lastStopLossPrices:   make(map[string]float64),
		stopCh:               make(chan struct{}),
		isRunning:            false,
	}
}

// SetOwner 更新监控器绑定的交易员（用于共享账户）
func (m *TrailingStopMonitor) SetOwner(trader *AutoTrader) {
	if m == nil || trader == nil {
		return
	}
	m.mu.Lock()
	m.trader = trader
	m.mu.Unlock()
}

func newPositionSnapshot(raw map[string]interface{}) (*positionSnapshot, error) {
	symbol, err := stringFromAny(raw["symbol"])
	if err != nil {
		return nil, fmt.Errorf("symbol 字段缺失: %w", err)
	}

	sideRaw, err := stringFromAny(raw["side"])
	if err != nil {
		return nil, fmt.Errorf("%s 缺少 side 字段: %w", symbol, err)
	}
	side := strings.ToLower(sideRaw)
	if side != "long" && side != "short" {
		return nil, fmt.Errorf("%s 无效方向: %s", symbol, sideRaw)
	}

	entryPrice, err := floatFromAny(raw["entryPrice"])
	if err != nil {
		return nil, fmt.Errorf("%s %s entryPrice 解析失败: %w", symbol, side, err)
	}

	markPrice, err := floatFromAny(raw["markPrice"])
	if err != nil {
		return nil, fmt.Errorf("%s %s markPrice 解析失败: %w", symbol, side, err)
	}

	quantity, err := floatFromAny(raw["positionAmt"])
	if err != nil {
		return nil, fmt.Errorf("%s %s positionAmt 解析失败: %w", symbol, side, err)
	}
	quantity = math.Abs(quantity)

	leverage := defaultLeverage
	if lev, err := floatFromAny(raw["leverage"]); err == nil && lev > 0 {
		leverage = int(math.Round(math.Max(lev, 1)))
	}

	return &positionSnapshot{
		Symbol:     symbol,
		Side:       side,
		EntryPrice: entryPrice,
		MarkPrice:  markPrice,
		Quantity:   quantity,
		Leverage:   leverage,
	}, nil
}

func stringFromAny(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return "", fmt.Errorf("字符串为空")
		}
		return trimmed, nil
	case fmt.Stringer:
		trimmed := strings.TrimSpace(v.String())
		if trimmed == "" {
			return "", fmt.Errorf("字符串为空")
		}
		return trimmed, nil
	case nil:
		return "", fmt.Errorf("值缺失")
	default:
		return "", fmt.Errorf("类型 %T 不能转换为字符串", value)
	}
}

func floatFromAny(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, fmt.Errorf("字符串为空")
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	case nil:
		return 0, fmt.Errorf("值缺失")
	default:
		return 0, fmt.Errorf("类型 %T 不能转换为浮点数", value)
	}
}

// Start 启动追踪止损监控器（独立goroutine，每5秒检查一次）
func (m *TrailingStopMonitor) Start() {
	m.mu.Lock()
	if m.isRunning {
		m.mu.Unlock()
		log.Println("⚠️  [追踪止损] 监控器已在运行，跳过启动")
		return
	}
	m.stopCh = make(chan struct{})
	m.isRunning = true
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		ticker := time.NewTicker(trailingCheckInterval)
		defer ticker.Stop()

		log.Printf("🚀 [追踪止损] 独立监控器启动（每%.0f秒检查一次）", trailingCheckInterval.Seconds())

		for {
			select {
			case <-ticker.C:
				// 获取当前持仓
				positions, err := m.trader.trader.GetPositions()
				if err != nil {
					log.Printf("❌ [追踪止损] 获取持仓失败: %v", err)
					continue
				}
				m.ProcessPositions(positions)

			case <-m.stopCh:
				log.Println("⏹  [追踪止损] 独立监控器停止")
				return
			}
		}
	}()
}

// Stop 停止追踪止损监控器
func (m *TrailingStopMonitor) Stop() {
	m.mu.Lock()
	if !m.isRunning {
		m.mu.Unlock()
		log.Println("⚠️  [追踪止损] 监控器未运行，跳过停止")
		return
	}
	m.isRunning = false
	m.mu.Unlock()

	close(m.stopCh)
	m.wg.Wait()
	log.Println("✅ [追踪止损] 独立监控器已停止")
}

// ProcessPositions 检查并更新动态止损
func (m *TrailingStopMonitor) ProcessPositions(positions []map[string]interface{}) {
	if len(positions) == 0 {
		return
	}

	var activePositions []*positionSnapshot
	for _, raw := range positions {
		snapshot, err := newPositionSnapshot(raw)
		if err != nil {
			log.Printf("⚠️  [追踪止损] 跳过无法解析的持仓: %v", err)
			continue
		}
		if snapshot.Quantity == 0 {
			continue
		}
		activePositions = append(activePositions, snapshot)
	}

	if len(activePositions) == 0 {
		log.Printf("📊 [追踪止损] 当前无持仓，跳过检查")
		return
	}

	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Printf("🔍 [追踪止损] 开始检查 %d 个持仓", len(activePositions))
	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	checkedCount := 0
	skippedCount := 0
	updatedCount := 0

	for _, snapshot := range activePositions {
		checkedCount++
		updated, skipped := m.processPositionSnapshot(snapshot, checkedCount, len(activePositions))
		if updated {
			updatedCount++
		}
		if skipped {
			skippedCount++
		}
	}

	log.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Printf("📊 [追踪止损] 检查完成 - 总计: %d | 已更新: %d | 已跳过: %d",
		checkedCount, updatedCount, skippedCount)
	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
}

func (m *TrailingStopMonitor) processPositionSnapshot(pos *positionSnapshot, index, total int) (updated bool, skipped bool) {
	if pos == nil {
		return false, true
	}

	sideLabel := strings.ToUpper(pos.Side)
	log.Printf("\n  📍 [%d/%d] 检查持仓: %s %s", index, total, pos.Symbol, sideLabel)
	log.Printf("      入场价格: %.4f | 当前价格: %.4f | 数量: %.4f | 杠杆: %dx",
		pos.EntryPrice, pos.MarkPrice, pos.Quantity, pos.Leverage)

	if pos.Quantity == 0 {
		log.Printf("  ⏭️  [%d/%d] %s %s - 空仓（数量=0），跳过", index, total, pos.Symbol, sideLabel)
		return false, true
	}

	currentProfitPct := pos.profitPct()
	priceDeltaPct := currentProfitPct / float64(pos.Leverage)
	log.Printf("      📈 收益率计算: %.2f%% (价格变动: %.2f%% × 杠杆: %dx)",
		currentProfitPct, priceDeltaPct, pos.Leverage)

	if currentProfitPct < minProfitThresholdPct {
		log.Printf("      ⏭️  收益率 %.2f%% < %.0f%%，不满足追踪止损条件，跳过",
			currentProfitPct, minProfitThresholdPct)
		return false, true
	}

	log.Printf("      ✅ 收益率 %.2f%% ≥ %.0f%%，符合追踪止损条件，继续处理...",
		currentProfitPct, minProfitThresholdPct)

	drawdownPct, retainPct := determineTrailingPercents(currentProfitPct)
	if drawdownPct == 0 || retainPct == 0 {
		log.Printf("      ⚠️  未能确定追踪配置，跳过")
		return false, true
	}
	log.Printf("      ⚙️  追踪配置: 允许回撤 %.0f%% | 保留收益 %.0f%%",
		drawdownPct*100, retainPct*100)

	posKey := pos.key()
	openTime := m.trader.positionFirstSeenTime[posKey]
	if openTime == 0 {
		openTime = time.Now().UnixMilli()
		log.Printf("      ⚠️  未找到开仓时间记录，使用当前时间")
	} else {
		duration := time.Since(time.Unix(openTime/1000, 0))
		log.Printf("      ⏱️  持仓时长: %v", duration.Round(time.Second))
	}

	log.Printf("      🔍 开始计算历史峰值价格（使用1分钟K线）...")
	peakPrice := m.calculatePeakPrice(pos.Symbol, pos.Side, pos.EntryPrice, pos.MarkPrice, openTime)

	log.Printf("      💡 计算追踪止损价格...")
	newStopLoss := m.calculateTrailingStopPrice(pos.Side, pos.EntryPrice, peakPrice, retainPct, drawdownPct)

	log.Printf("      🔍 验证止损价格有效性...")
	isValid, triggerClose := m.isStopLossValid(pos.Side, pos.EntryPrice, newStopLoss, pos.MarkPrice)
	if triggerClose {
		log.Printf("      🚨 当前价格已触发追踪止损，执行紧急平仓流程")
		if err := m.executeMarketClose(pos.Symbol, pos.Side, pos.MarkPrice); err != nil {
			log.Printf("      ❌ 紧急平仓失败: %v", err)
			return false, false
		}
		log.Printf("      ✅ 紧急平仓完成，结束此持仓检查")
		return true, false
	}

	if !isValid {
		log.Printf("      ❌ 止损价格验证失败，跳过此持仓")
		return false, true
	}

	log.Printf("      ✅ 止损价格验证通过")
	log.Printf("\n      🎯 [追踪止损决策] %s %s", pos.Symbol, sideLabel)
	log.Printf("         收益率: %.2f%%", currentProfitPct)
	log.Printf("         入场价: %.4f", pos.EntryPrice)
	log.Printf("         峰值价: %.4f", peakPrice)
	log.Printf("         当前价: %.4f", pos.MarkPrice)
	log.Printf("         新止损: %.4f", newStopLoss)

	log.Printf("      🔧 正在设置止损单...")
	if err := m.updateStopLoss(pos.Symbol, pos.Side, pos.Quantity, newStopLoss, pos.MarkPrice, retainPct, drawdownPct); err != nil {
		log.Printf("      ❌ 设置止损单失败: %v", err)
		return false, false
	}

	log.Printf("      ✅ 成功设置动态止损价 %.4f", newStopLoss)
	return true, false
}

// calculatePeakPrice 计算历史最高价/最低价（使用1分钟K线，从开仓时间开始）
func (m *TrailingStopMonitor) calculatePeakPrice(symbol, side string, entryPrice, currentPrice float64,
	openTime int64) float64 {

	posKey := symbol + "_" + side
	var peakPrice float64

	if side == "long" {
		// 多单：找最高价
		peakPrice = entryPrice
		log.Printf("         [峰值追踪-多单] 初始峰值 = 入场价 %.4f", peakPrice)

		// 1. 检查当前价格
		if currentPrice > peakPrice {
			log.Printf("         [峰值追踪-多单] 当前价 %.4f > 峰值 %.4f，更新峰值", currentPrice, peakPrice)
			peakPrice = currentPrice
		} else {
			log.Printf("         [峰值追踪-多单] 当前价 %.4f ≤ 峰值 %.4f，保持峰值", currentPrice, peakPrice)
		}

		// 2. 从市场监控器获取1分钟K线数据
		klines1m, err := market.WSMonitorCli.GetCurrentKlines(symbol, "1m")
		if err != nil {
			log.Printf("         [峰值追踪-多单] ⚠️ 获取1分钟K线失败: %v，使用当前价格", err)
		} else {
			// 过滤开仓时间之后的K线
			var filteredKlines []market.Kline
			for _, kline := range klines1m {
				if kline.OpenTime >= openTime {
					filteredKlines = append(filteredKlines, kline)
				}
			}

			if len(filteredKlines) > 0 {
				log.Printf("         [峰值追踪-多单] 找到 %d 根开仓时间后的1分钟K线（总共 %d 根）",
					len(filteredKlines), len(klines1m))

				maxKlinePrice := peakPrice
				for _, kline := range filteredKlines {
					if kline.High > maxKlinePrice {
						maxKlinePrice = kline.High
					}
				}

				if maxKlinePrice > peakPrice {
					log.Printf("         [峰值追踪-多单] K线最高价 %.4f > 峰值 %.4f（检查了%d根K线），更新峰值",
						maxKlinePrice, peakPrice, len(filteredKlines))
					peakPrice = maxKlinePrice
				} else {
					log.Printf("         [峰值追踪-多单] K线最高价 %.4f ≤ 峰值 %.4f（检查了%d根K线），保持峰值",
						maxKlinePrice, peakPrice, len(filteredKlines))
				}
			} else {
				log.Printf("         [峰值追踪-多单] ⚠️ 未找到开仓时间后的K线，使用当前价格")
			}
		}

		// 3. 检查缓存中的历史最高价
		m.mu.RLock()
		cachedPeak, exists := m.historicalPeakPrices[posKey]
		m.mu.RUnlock()
		if exists {
			if cachedPeak > peakPrice {
				log.Printf("         [峰值追踪-多单] 缓存峰值 %.4f > 当前峰值 %.4f，使用缓存值", cachedPeak, peakPrice)
				peakPrice = cachedPeak
			} else {
				log.Printf("         [峰值追踪-多单] 缓存峰值 %.4f ≤ 当前峰值 %.4f，更新缓存", cachedPeak, peakPrice)
			}
		} else {
			log.Printf("         [峰值追踪-多单] 首次记录峰值 %.4f", peakPrice)
		}

		// 4. 更新缓存
		m.mu.Lock()
		m.historicalPeakPrices[posKey] = peakPrice
		m.mu.Unlock()

		log.Printf("         [峰值追踪-多单] ✅ 最终峰值价格: %.4f", peakPrice)

	} else {
		// 空单：找最低价（对空单来说最低价是最佳收益点）
		peakPrice = entryPrice
		log.Printf("         [峰值追踪-空单] 初始峰值 = 入场价 %.4f", peakPrice)

		// 1. 检查当前价格
		if currentPrice < peakPrice {
			log.Printf("         [峰值追踪-空单] 当前价 %.4f < 峰值 %.4f，更新峰值", currentPrice, peakPrice)
			peakPrice = currentPrice
		} else {
			log.Printf("         [峰值追踪-空单] 当前价 %.4f ≥ 峰值 %.4f，保持峰值", currentPrice, peakPrice)
		}

		// 2. 从市场监控器获取1分钟K线数据
		klines1m, err := market.WSMonitorCli.GetCurrentKlines(symbol, "1m")
		if err != nil {
			log.Printf("         [峰值追踪-空单] ⚠️ 获取1分钟K线失败: %v，使用当前价格", err)
		} else {
			// 过滤开仓时间之后的K线
			var filteredKlines []market.Kline
			for _, kline := range klines1m {
				if kline.OpenTime >= openTime {
					filteredKlines = append(filteredKlines, kline)
				}
			}

			if len(filteredKlines) > 0 {
				log.Printf("         [峰值追踪-空单] 找到 %d 根开仓时间后的1分钟K线（总共 %d 根）",
					len(filteredKlines), len(klines1m))

				minKlinePrice := peakPrice
				for _, kline := range filteredKlines {
					if kline.Low < minKlinePrice {
						minKlinePrice = kline.Low
					}
				}

				if minKlinePrice < peakPrice {
					log.Printf("         [峰值追踪-空单] K线最低价 %.4f < 峰值 %.4f（检查了%d根K线），更新峰值",
						minKlinePrice, peakPrice, len(filteredKlines))
					peakPrice = minKlinePrice
				} else {
					log.Printf("         [峰值追踪-空单] K线最低价 %.4f ≥ 峰值 %.4f（检查了%d根K线），保持峰值",
						minKlinePrice, peakPrice, len(filteredKlines))
				}
			} else {
				log.Printf("         [峰值追踪-空单] ⚠️ 未找到开仓时间后的K线，使用当前价格")
			}
		}

		// 3. 检查缓存中的历史最低价
		m.mu.RLock()
		cachedPeak, exists := m.historicalPeakPrices[posKey]
		m.mu.RUnlock()
		if exists {
			if cachedPeak < peakPrice {
				log.Printf("         [峰值追踪-空单] 缓存峰值 %.4f < 当前峰值 %.4f，使用缓存值", cachedPeak, peakPrice)
				peakPrice = cachedPeak
			} else {
				log.Printf("         [峰值追踪-空单] 缓存峰值 %.4f ≥ 当前峰值 %.4f，更新缓存", cachedPeak, peakPrice)
			}
		} else {
			log.Printf("         [峰值追踪-空单] 首次记录峰值 %.4f", peakPrice)
		}

		// 4. 更新缓存
		m.mu.Lock()
		m.historicalPeakPrices[posKey] = peakPrice
		m.mu.Unlock()

		log.Printf("         [峰值追踪-空单] ✅ 最终峰值价格: %.4f", peakPrice)
	}

	return peakPrice
}

// calculateTrailingStopPrice 计算追踪止损价格（根据收益区间动态调整回撤）
func (m *TrailingStopMonitor) calculateTrailingStopPrice(side string, entryPrice, peakPrice, retainPct, drawdownPct float64) float64 {
	var stopLoss float64
	if side == "long" {
		// 多单：
		// 收益空间 = 峰值价 - 入场价
		// 止损价 = 入场价 + 收益空间 × 保留收益比例
		profitSpace := peakPrice - entryPrice
		stopLoss = entryPrice + profitSpace*retainPct

		log.Printf("         [止损计算-多单] 收益空间: %.4f (峰值 %.4f - 入场 %.4f)",
			profitSpace, peakPrice, entryPrice)
		log.Printf("         [止损计算-多单] 允许回撤: %.0f%% | 保留收益: %.2f%% | 止损价: %.4f + %.4f × %.0f%% = %.4f",
			drawdownPct*100, retainPct*100, entryPrice, profitSpace, retainPct*100, stopLoss)
	} else {
		// 空单：
		// 收益空间 = 入场价 - 峰值价
		// 止损价 = 入场价 - 收益空间 × 保留收益比例
		profitSpace := entryPrice - peakPrice
		stopLoss = entryPrice - profitSpace*retainPct

		log.Printf("         [止损计算-空单] 收益空间: %.4f (入场 %.4f - 峰值 %.4f)",
			profitSpace, entryPrice, peakPrice)
		log.Printf("         [止损计算-空单] 允许回撤: %.0f%% | 保留收益: %.2f%% | 止损价: %.4f - %.4f × %.0f%% = %.4f",
			drawdownPct*100, retainPct*100, entryPrice, profitSpace, retainPct*100, stopLoss)
	}

	return stopLoss
}

// isStopLossValid 验证止损价是否有效，并返回是否需要立即触发紧急平仓
func (m *TrailingStopMonitor) isStopLossValid(side string, entryPrice, newStopLoss, currentPrice float64) (bool, bool) {
	log.Printf("         [验证] 止损价: %.4f | 入场价: %.4f | 当前价: %.4f", newStopLoss, entryPrice, currentPrice)

	if side == "long" {
		// 多单止损必须满足：
		// 1. 止损价高于入场价（保护利润）
		log.Printf("         [验证-多单] 检查1: 止损价 %.4f > 入场价 %.4f?", newStopLoss, entryPrice)
		if newStopLoss <= entryPrice {
			log.Printf("         [验证-多单] ❌ 失败: 止损价 %.4f ≤ 入场价 %.4f（无法保护利润）", newStopLoss, entryPrice)
			return false, false
		}
		log.Printf("         [验证-多单] ✅ 通过: 止损价高于入场价，可保护利润")

		// 2. 止损价低于当前价（合理性检查）
		log.Printf("         [验证-多单] 检查2: 止损价 %.4f < 当前价 %.4f?", newStopLoss, currentPrice)
		if newStopLoss >= currentPrice {
			log.Printf("         [验证-多单] ❌ 失败: 止损价 %.4f ≥ 当前价 %.4f（会立即触发）", newStopLoss, currentPrice)
			return false, true
		}
		log.Printf("         [验证-多单] ✅ 通过: 止损价低于当前价，合理")

	} else {
		// 空单止损必须满足：
		// 1. 止损价低于入场价（保护利润）
		log.Printf("         [验证-空单] 检查1: 止损价 %.4f < 入场价 %.4f?", newStopLoss, entryPrice)
		if newStopLoss >= entryPrice {
			log.Printf("         [验证-空单] ❌ 失败: 止损价 %.4f ≥ 入场价 %.4f（无法保护利润）", newStopLoss, entryPrice)
			return false, false
		}
		log.Printf("         [验证-空单] ✅ 通过: 止损价低于入场价，可保护利润")

		// 2. 止损价高于当前价（合理性检查）
		log.Printf("         [验证-空单] 检查2: 止损价 %.4f > 当前价 %.4f?", newStopLoss, currentPrice)
		if newStopLoss <= currentPrice {
			log.Printf("         [验证-空单] ❌ 失败: 止损价 %.4f ≤ 当前价 %.4f（会立即触发）", newStopLoss, currentPrice)
			return false, true
		}
		log.Printf("         [验证-空单] ✅ 通过: 止损价高于当前价，合理")
	}

	log.Printf("         [验证] ✅ 所有检查通过，止损价有效")
	return true, false
}

// updateStopLoss 更新止损价（使用统一的止损更新逻辑）
func (m *TrailingStopMonitor) updateStopLoss(symbol, side string, quantity, newStopLoss, currentPrice, retainPct, drawdownPct float64) error {
	posKey := symbol + "_" + side

	// 🚨 优先检查：止损价是否已被触发（价格跌破/突破止损线）
	stopLossTriggered := false
	if side == "long" {
		// 多单：当前价格 <= 止损价，说明已触发止损
		if currentPrice <= newStopLoss {
			log.Printf("         [追踪止损] 🚨 多单止损已触发！当前价 %.4f ≤ 止损价 %.4f", currentPrice, newStopLoss)
			stopLossTriggered = true
		}
	} else {
		// 空单：当前价格 >= 止损价，说明已触发止损
		if currentPrice >= newStopLoss {
			log.Printf("         [追踪止损] 🚨 空单止损已触发！当前价 %.4f ≥ 止损价 %.4f", currentPrice, newStopLoss)
			stopLossTriggered = true
		}
	}

	// 如果止损已触发，直接执行市价平仓
	if stopLossTriggered {
		log.Printf("         [追踪止损] 🔥 执行紧急市价平仓: %s %s", symbol, strings.ToUpper(side))
		if err := m.executeMarketClose(symbol, side, currentPrice); err != nil {
			log.Printf("         [追踪止损] ❌ 紧急平仓失败: %v", err)
			return fmt.Errorf("紧急平仓失败: %w", err)
		}
		log.Printf("         [追踪止损] ✅ 紧急平仓成功，止损已触发")
		return nil
	}

	// 检查上次设置的止损价，避免重复调用API
	m.mu.RLock()
	lastStopLoss, exists := m.lastStopLossPrices[posKey]
	m.mu.RUnlock()

	if exists {
		log.Printf("         [追踪止损] 检测到上次止损价: %.4f", lastStopLoss)

		// 判断新止损价是否更优
		shouldUpdate := false
		if side == "long" {
			// 多单：新止损价必须高于上次止损价（止损上移）
			if newStopLoss > lastStopLoss {
				log.Printf("         [追踪止损] 多单止损上移: %.4f -> %.4f (提升 %.4f)",
					lastStopLoss, newStopLoss, newStopLoss-lastStopLoss)
				shouldUpdate = true
			} else {
				log.Printf("         [追踪止损] ⏭️  多单新止损 %.4f ≤ 上次 %.4f，无需更新（避免重复调用API）",
					newStopLoss, lastStopLoss)
			}
		} else {
			// 空单：新止损价必须低于上次止损价（止损下移）
			if newStopLoss < lastStopLoss {
				log.Printf("         [追踪止损] 空单止损下移: %.4f -> %.4f (降低 %.4f)",
					lastStopLoss, newStopLoss, lastStopLoss-newStopLoss)
				shouldUpdate = true
			} else {
				log.Printf("         [追踪止损] ⏭️  空单新止损 %.4f ≥ 上次 %.4f，无需更新（避免重复调用API）",
					newStopLoss, lastStopLoss)
			}
		}

		if !shouldUpdate {
			return nil
		}
	} else {
		log.Printf("         [追踪止损] 首次设置止损价: %.4f", newStopLoss)
	}

	log.Printf("         [追踪止损] 调用统一止损更新接口...")
	log.Printf("         [追踪止损] 币种: %s | 方向: %s | 数量: %.4f | 止损价: %.4f",
		symbol, strings.ToUpper(side), quantity, newStopLoss)

	// 构建 Decision 对象（用于 executeUpdateStopLossWithRecord）
	d := &decision.Decision{
		Symbol:      symbol,
		Action:      "update_stop_loss",
		NewStopLoss: newStopLoss,
		Reasoning:   fmt.Sprintf("追踪止损自动调整: 允许%.0f%%回撤（保留%.0f%%收益），止损价 %.4f", drawdownPct*100, retainPct*100, newStopLoss),
	}

	// 构建 DecisionAction 记录（用于日志记录）
	actionRecord := &logger.DecisionAction{
		Action:    "update_stop_loss",
		Symbol:    symbol,
		Quantity:  0, // executeUpdateStopLossWithRecord 内部会重新获取
		Leverage:  0,
		Price:     currentPrice,
		Timestamp: time.Now(),
		Success:   false,
	}

	// 调用 AutoTrader 的统一止损更新方法
	// 该方法会自动处理：
	// 1. 获取持仓信息和验证
	// 2. 防御性检查（价格合理性）
	// 3. 双向持仓检测
	// 4. 取消旧止损单
	// 5. 设置新止损单
	// 6. 完整的决策日志记录
	err := m.trader.executeUpdateStopLossWithRecord(d, actionRecord)
	if err != nil {
		log.Printf("         [追踪止损] ❌ 调用统一止损更新接口失败: %v", err)
		return fmt.Errorf("追踪止损更新失败: %w", err)
	}

	// 成功设置后，缓存新的止损价
	m.mu.Lock()
	m.lastStopLossPrices[posKey] = newStopLoss
	m.mu.Unlock()

	log.Printf("         [追踪止损] ✅ 通过统一接口成功设置止损，已缓存止损价 %.4f", newStopLoss)
	return nil
}

// executeMarketClose 执行紧急市价平仓（止损触发时使用）
func (m *TrailingStopMonitor) executeMarketClose(symbol, side string, currentPrice float64) error {
	log.Printf("         [紧急平仓] 开始执行市价平仓: %s %s (当前价: %.4f)", symbol, strings.ToUpper(side), currentPrice)

	var order map[string]interface{}
	var err error

	// 执行平仓
	if side == "long" {
		order, err = m.trader.trader.CloseLong(symbol, 0) // 0 = 全部平仓
		if err != nil {
			return fmt.Errorf("平多仓失败: %w", err)
		}
		log.Printf("         [紧急平仓] 平多仓成功，订单ID: %v", order["orderId"])
	} else {
		order, err = m.trader.trader.CloseShort(symbol, 0) // 0 = 全部平仓
		if err != nil {
			return fmt.Errorf("平空仓失败: %w", err)
		}
		log.Printf("         [紧急平仓] 平空仓成功，订单ID: %v", order["orderId"])
	}

	// 清除追踪止损缓存
	m.ClearPosition(symbol, side)

	// 记录决策日志（用于回溯分析）
	actionRecord := &logger.DecisionAction{
		Action:    fmt.Sprintf("emergency_close_%s", side),
		Symbol:    symbol,
		Quantity:  0,
		Leverage:  0,
		Price:     currentPrice,
		Timestamp: time.Now(),
		Success:   true,
	}

	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	// 创建决策记录
	record := &logger.DecisionRecord{
		ExecutionLog: []string{fmt.Sprintf("🚨 追踪止损触发紧急平仓: %s %s", symbol, side)},
		Success:      true,
		Decisions:    []logger.DecisionAction{*actionRecord},
	}

	// 保存到决策日志
	if err := m.trader.decisionLogger.LogDecision(record); err != nil {
		log.Printf("         [紧急平仓] ⚠️  保存决策记录失败: %v", err)
	}

	log.Printf("         [紧急平仓] ✅ 完成: %s %s 已市价平仓", symbol, strings.ToUpper(side))
	return nil
}

// ClearPosition 清除持仓缓存（平仓后调用）
func (m *TrailingStopMonitor) ClearPosition(symbol, side string) {
	posKey := symbol + "_" + side
	m.mu.Lock()
	defer m.mu.Unlock()

	// 清除峰值缓存
	if peakPrice, exists := m.historicalPeakPrices[posKey]; exists {
		delete(m.historicalPeakPrices, posKey)
		log.Printf("🧹 [追踪止损] 清除 %s 峰值缓存 (峰值价: %.4f)", posKey, peakPrice)
	} else {
		log.Printf("🧹 [追踪止损] %s 峰值缓存不存在", posKey)
	}

	// 清除止损价缓存
	if stopLoss, exists := m.lastStopLossPrices[posKey]; exists {
		delete(m.lastStopLossPrices, posKey)
		log.Printf("🧹 [追踪止损] 清除 %s 止损价缓存 (止损价: %.4f)", posKey, stopLoss)
	} else {
		log.Printf("🧹 [追踪止损] %s 止损价缓存不存在", posKey)
	}
}
