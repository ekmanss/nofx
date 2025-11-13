package trader

import (
	"fmt"
	"log"
	"math"
	"nofx/decision"
	"nofx/logger"
	"strings"
	"sync"
	"time"
)

// TrailingStopMonitor 动态追踪止损监控器
type TrailingStopMonitor struct {
	trader             *AutoTrader
	riskStates         map[string]*riskStageInfo
	lastStopLossPrices map[string]float64 // symbol_side -> 上次设置的止损价（避免重复调用API）
	mu                 sync.RWMutex
	stopCh             chan struct{} // 用于停止监控goroutine
	wg                 sync.WaitGroup
	isRunning          bool
}

const (
	trailingCheckInterval = 5 * time.Second
	defaultLeverage       = 5

	rStageInitial   = iota // 尚未达到 +1R
	rStageBreakeven        // +1R，止损移至开仓价
	rStageLockOneR         // +2R，止损锁定 +1R
	rStageATR              // +3R 启动 ATR Trailing
)

type riskStageInfo struct {
	InitialStop float64
	Stage       int
}

// NewTrailingStopMonitor 创建动态止损监控器
func NewTrailingStopMonitor(trader *AutoTrader) *TrailingStopMonitor {
	return &TrailingStopMonitor{
		trader:             trader,
		riskStates:         make(map[string]*riskStageInfo),
		lastStopLossPrices: make(map[string]float64),
		stopCh:             make(chan struct{}),
		isRunning:          false,
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

// RegisterInitialStop 记录某个持仓的初始止损，用于R-based分段管理
func (m *TrailingStopMonitor) RegisterInitialStop(symbol, side string, stop float64) {
	if m == nil || symbol == "" || stop <= 0 {
		return
	}

	posKey := symbol + "_" + strings.ToLower(side)

	m.mu.Lock()
	m.riskStates[posKey] = &riskStageInfo{InitialStop: stop, Stage: rStageInitial}
	delete(m.lastStopLossPrices, posKey) // 避免复用旧止损
	m.mu.Unlock()

	log.Printf("🆕 [追踪止损] 记录初始止损: %s %s → %.4f (阶段重置)", symbol, strings.ToUpper(side), stop)
}

func (m *TrailingStopMonitor) getRiskState(posKey string) (*riskStageInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.riskStates[posKey]
	if !ok {
		return nil, false
	}
	copied := *info
	return &copied, true
}

func (m *TrailingStopMonitor) setRiskStage(posKey string, stage int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info, ok := m.riskStates[posKey]; ok {
		info.Stage = stage
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
		m.cleanupInactivePositions(nil)
		return
	}

	var activePositions []*positionSnapshot
	activeKeys := make(map[string]struct{})
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
		activeKeys[snapshot.key()] = struct{}{}
	}

	m.cleanupInactivePositions(activeKeys)

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

// cleanupInactivePositions 移除已平仓持仓的缓存，避免沿用历史峰值/止损
func (m *TrailingStopMonitor) cleanupInactivePositions(activeKeys map[string]struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.riskStates) == 0 && len(m.lastStopLossPrices) == 0 {
		return
	}

	keep := func(key string) bool {
		if len(activeKeys) == 0 {
			return false
		}
		_, ok := activeKeys[key]
		return ok
	}

	for key := range m.lastStopLossPrices {
		if keep(key) {
			continue
		}
		delete(m.lastStopLossPrices, key)
		log.Printf("🧹 [追踪止损] 移除失效止损缓存: %s", key)
	}

	for key := range m.riskStates {
		if keep(key) {
			continue
		}
		delete(m.riskStates, key)
		log.Printf("🧹 [追踪止损] 移除失效风险分段缓存: %s", key)
	}
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

	posKey := pos.key()
	riskInfo, ok := m.getRiskState(posKey)
	if !ok {
		log.Printf("      ⏭️  未记录初始止损，无法计算R倍数，跳过")
		return false, true
	}

	riskDistance := math.Abs(pos.EntryPrice - riskInfo.InitialStop)
	if riskDistance == 0 {
		log.Printf("      ⏭️  入场价 %.4f 与初始止损 %.4f 重合，无法计算1R，跳过", pos.EntryPrice, riskInfo.InitialStop)
		return false, true
	}

	var currentR float64
	if pos.Side == "long" {
		currentR = (pos.MarkPrice - pos.EntryPrice) / riskDistance
	} else {
		currentR = (pos.EntryPrice - pos.MarkPrice) / riskDistance
	}

	log.Printf("      🧮 初始止损: %.4f | 1R距离: %.4f | 当前: %.2fR | 阶段: %s",
		riskInfo.InitialStop, riskDistance, currentR, formatStageName(riskInfo.Stage))

	nextStage := riskInfo.Stage
	var (
		shouldUpdate bool
		newStopLoss  float64
		reason       string
	)

	switch riskInfo.Stage {
	case rStageInitial:
		if currentR >= 1.0 {
			shouldUpdate = true
			nextStage = rStageBreakeven
			newStopLoss = pos.EntryPrice
			reason = fmt.Sprintf("R-based 分段: +1R 达成，止损移至开仓价 %.4f", newStopLoss)
			log.Printf("      ✅ 达成 +1R，准备将止损移动到开仓价")
		} else {
			log.Printf("      ⏳ 当前 %.2fR，等待达到 +1R 再移动止损", currentR)
			return false, true
		}
	case rStageBreakeven:
		if currentR >= 2.0 {
			shouldUpdate = true
			nextStage = rStageLockOneR
			if pos.Side == "long" {
				newStopLoss = pos.EntryPrice + riskDistance
			} else {
				newStopLoss = pos.EntryPrice - riskDistance
			}
			reason = fmt.Sprintf("R-based 分段: +2R 达成，止损锁定 +1R (%.4f)", newStopLoss)
			log.Printf("      ✅ 达成 +2R，止损将移动到 +1R 位置")
		} else {
			log.Printf("      ⏳ 当前 %.2fR，等待达到 +2R", currentR)
			return false, true
		}
	case rStageLockOneR:
		if currentR >= 3.0 {
			log.Printf("      🎯 +3R 达成，启动 ATR Trailing")
			atrStop, atrReason, err := m.calculateATRTrailingStop(pos, riskDistance)
			if err != nil {
				log.Printf("      ⚠️  ATR Trailing 数据不足: %v", err)
				return false, true
			}
			shouldUpdate = true
			nextStage = rStageATR
			newStopLoss = atrStop
			reason = atrReason
		}
		if !shouldUpdate {
			log.Printf("      ⏳ 当前 %.2fR，等待达到 +3R 以启动 ATR Trailing", currentR)
			return false, true
		}
	case rStageATR:
		atrStop, atrReason, err := m.calculateATRTrailingStop(pos, riskDistance)
		if err != nil {
			log.Printf("      ⚠️  ATR Trailing 计算失败: %v", err)
			return false, true
		}
		shouldUpdate = true
		nextStage = rStageATR
		newStopLoss = atrStop
		reason = atrReason
	default:
		log.Printf("      ⚠️ 未知分段状态 %d，跳过", riskInfo.Stage)
		return false, true
	}

	if !shouldUpdate {
		return false, true
	}

	log.Printf("      🔍 验证止损价格有效性...")
	isValid, triggerClose := m.isStopLossValid(pos.Side, pos.EntryPrice, newStopLoss, pos.MarkPrice)
	if triggerClose {
		log.Printf("      🚨 当前价格已触及新止损，执行紧急平仓")
		if err := m.executeMarketClose(pos.Symbol, pos.Side, pos.MarkPrice); err != nil {
			log.Printf("      ❌ 紧急平仓失败: %v", err)
			return false, false
		}
		log.Printf("      ✅ 紧急平仓完成")
		return true, false
	}

	if !isValid {
		log.Printf("      ❌ 止损价格验证失败，跳过此持仓")
		return false, true
	}

	log.Printf("      ✅ 止损价格验证通过，准备更新止损 → %.4f", newStopLoss)
	if err := m.updateStopLoss(pos.Symbol, pos.Side, pos.Quantity, newStopLoss, pos.MarkPrice, reason); err != nil {
		log.Printf("      ❌ 设置止损单失败: %v", err)
		return false, false
	}

	m.setRiskStage(posKey, nextStage)
	log.Printf("      ✅ 成功设置分段止损，阶段切换为 %s", formatStageName(nextStage))
	return true, false
}

// isStopLossValid 验证止损价是否有效，并返回是否需要立即触发紧急平仓
func (m *TrailingStopMonitor) isStopLossValid(side string, entryPrice, newStopLoss, currentPrice float64) (bool, bool) {
	log.Printf("         [验证] 止损价: %.4f | 入场价: %.4f | 当前价: %.4f", newStopLoss, entryPrice, currentPrice)

	if side == "long" {
		// 多单止损必须满足：
		// 1. 止损价不低于入场价（允许等于开仓价实现保本）
		log.Printf("         [验证-多单] 检查1: 止损价 %.4f ≥ 入场价 %.4f?", newStopLoss, entryPrice)
		if newStopLoss < entryPrice {
			log.Printf("         [验证-多单] ❌ 失败: 止损价 %.4f < 入场价 %.4f（无法保护利润）", newStopLoss, entryPrice)
			return false, false
		}
		log.Printf("         [验证-多单] ✅ 通过: 止损价不低于入场价，可保护利润/保本")

		// 2. 止损价低于当前价（合理性检查）
		log.Printf("         [验证-多单] 检查2: 止损价 %.4f < 当前价 %.4f?", newStopLoss, currentPrice)
		if newStopLoss >= currentPrice {
			log.Printf("         [验证-多单] ❌ 失败: 止损价 %.4f ≥ 当前价 %.4f（会立即触发）", newStopLoss, currentPrice)
			return false, true
		}
		log.Printf("         [验证-多单] ✅ 通过: 止损价低于当前价，合理")

	} else {
		// 空单止损必须满足：
		// 1. 止损价不高于入场价（允许等于开仓价实现保本）
		log.Printf("         [验证-空单] 检查1: 止损价 %.4f ≤ 入场价 %.4f?", newStopLoss, entryPrice)
		if newStopLoss > entryPrice {
			log.Printf("         [验证-空单] ❌ 失败: 止损价 %.4f > 入场价 %.4f（无法保护利润）", newStopLoss, entryPrice)
			return false, false
		}
		log.Printf("         [验证-空单] ✅ 通过: 止损价不高于入场价，可保护利润/保本")

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
func (m *TrailingStopMonitor) updateStopLoss(symbol, side string, quantity, newStopLoss, currentPrice float64, reason string) error {
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
	if reason == "" {
		reason = fmt.Sprintf("R-based 分段追踪: 止损调整至 %.4f", newStopLoss)
	}

	// 构建 Decision 对象（用于 executeUpdateStopLossWithRecord）
	d := &decision.Decision{
		Symbol:      symbol,
		Action:      "update_stop_loss",
		NewStopLoss: newStopLoss,
		Reasoning:   reason,
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

	// 清除止损价缓存
	if stopLoss, exists := m.lastStopLossPrices[posKey]; exists {
		delete(m.lastStopLossPrices, posKey)
		log.Printf("🧹 [追踪止损] 清除 %s 止损价缓存 (止损价: %.4f)", posKey, stopLoss)
	} else {
		log.Printf("🧹 [追踪止损] %s 止损价缓存不存在", posKey)
	}

	if risk, exists := m.riskStates[posKey]; exists {
		delete(m.riskStates, posKey)
		log.Printf("🧹 [追踪止损] 清除 %s 风险分段缓存 (初始止损: %.4f)", posKey, risk.InitialStop)
	}
}

func formatStageName(stage int) string {
	switch stage {
	case rStageInitial:
		return "阶段0 (等待+1R)"
	case rStageBreakeven:
		return "阶段1 (+1R已触发)"
	case rStageLockOneR:
		return "阶段2 (+2R已触发)"
	case rStageATR:
		return "阶段3 (ATR Trailing)"
	default:
		return fmt.Sprintf("阶段%d", stage)
	}
}
