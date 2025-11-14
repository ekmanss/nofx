package trailingstop

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
	owner         Owner
	atrCalculator *ATRTrailingCalculator
	riskRegistry  *riskRegistry
	mu            sync.RWMutex
	stopCh        chan struct{} // 用于停止监控goroutine
	wg            sync.WaitGroup
	isRunning     bool
}

const (
	trailingCheckInterval = 5 * time.Second
)

func (m *TrailingStopMonitor) tradingClient() TradingClient {
	if m == nil || m.owner == nil {
		return nil
	}
	return m.owner.TradingClient()
}

// NewTrailingStopMonitor 创建动态止损监控器
func NewTrailingStopMonitor(owner Owner) *TrailingStopMonitor {
	return NewTrailingStopMonitorWithConfig(owner, nil)
}

// NewTrailingStopMonitorWithConfig allows callers to customize the trailing-stop parameters.
func NewTrailingStopMonitorWithConfig(owner Owner, cfg *Config) *TrailingStopMonitor {
	return &TrailingStopMonitor{
		owner:         owner,
		atrCalculator: NewATRTrailingCalculatorWithConfig(nil, cfg),
		riskRegistry:  newRiskRegistry(),
		stopCh:        make(chan struct{}),
		isRunning:     false,
	}
}

// SetOwner 更新监控器绑定的交易员（用于共享账户）
func (m *TrailingStopMonitor) SetOwner(owner Owner) {
	if m == nil || owner == nil {
		return
	}
	m.mu.Lock()
	m.owner = owner
	m.mu.Unlock()
}

// RegisterInitialStop 记录某个持仓的初始止损，用于R-based分段管理
func (m *TrailingStopMonitor) RegisterInitialStop(symbol, side string, stop float64) {
	if m == nil || symbol == "" || stop <= 0 {
		return
	}

	if m.riskRegistry == nil {
		m.riskRegistry = newRiskRegistry()
	}
	m.riskRegistry.registerInitialStop(symbol, side, stop)

	log.Printf("🆕 [追踪止损] 记录初始止损: %s %s → %.4f", symbol, strings.ToUpper(side), stop)
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
				client := m.tradingClient()
				if client == nil {
					log.Printf("❌ [追踪止损] 无法访问交易接口，等待下次重试")
					continue
				}
				positions, err := client.GetPositions()
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

	var activePositions []*Snapshot
	activeKeys := make(map[string]struct{})
	for _, raw := range positions {
		snapshot, err := NewSnapshot(raw)
		if err != nil {
			log.Printf("⚠️  [追踪止损] 跳过无法解析的持仓: %v", err)
			continue
		}
		if snapshot.Quantity == 0 {
			continue
		}
		activePositions = append(activePositions, snapshot)
		activeKeys[snapshot.Key()] = struct{}{}
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
	if m == nil || m.riskRegistry == nil {
		return
	}

	removed := m.riskRegistry.cleanup(activeKeys)
	for _, entry := range removed {
		log.Printf("🧹 [追踪止损] 移除失效风险分段缓存: %s (初始止损: %.4f)", entry.key, entry.initialStop)
	}
}

func (m *TrailingStopMonitor) processPositionSnapshot(pos *Snapshot, index, total int) (updated bool, skipped bool) {
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

	if m.riskRegistry == nil {
		log.Printf("      ⏭️  未初始化风险缓存，跳过")
		return false, true
	}

	posKey := pos.Key()
	riskInfo, ok := m.riskRegistry.snapshot(posKey)
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

	m.riskRegistry.updatePeakAndMaxR(pos, posKey, currentR)
	if snapshot, exists := m.riskRegistry.snapshot(posKey); exists {
		riskInfo = snapshot
	}

	log.Printf("      🧮 初始止损: %.4f | 1R距离: %.4f | 当前: %.2fR | 峰值R: %.2fR",
		riskInfo.InitialStop, riskDistance, currentR, riskInfo.MaxR)

	prevStop := riskInfo.InitialStop
	hasPrevStop := false
	var stopQueryErr error
	if stop, exists, err := m.getCurrentStopLoss(pos.Symbol, pos.Side); err != nil {
		stopQueryErr = err
		log.Printf("      ⚠️ 获取当前止损失败，将尝试使用记录值: %v", err)
	} else if exists {
		prevStop = stop
		hasPrevStop = true
		m.riskRegistry.recordStopLoss(posKey, stop)
		log.Printf("      📌 交易所当前止损: %.4f", prevStop)
	}

	if !hasPrevStop {
		if riskInfo.HasRecordedStop && riskInfo.LastRecordedStop > 0 {
			prevStop = riskInfo.LastRecordedStop
			hasPrevStop = true
			log.Printf("      📌 使用上次记录的止损 %.4f 作为基准", prevStop)
		} else if stopQueryErr == nil {
			log.Printf("      📌 交易所暂无止损单，使用初始止损 %.4f 作为基准", prevStop)
		} else {
			log.Printf("      📌 未获取到止损信息，退回初始止损 %.4f 作为基准", prevStop)
		}
	}

	riskSnapshot := &RiskSnapshot{
		InitialStop: riskInfo.InitialStop,
		PeakPrice:   riskInfo.PeakPrice,
		MaxR:        riskInfo.MaxR,
	}
	newStopLoss, reason, err := m.atrCalculator.Calculate(pos, riskSnapshot, prevStop, hasPrevStop)
	if err != nil {
		log.Printf("      ⚠️ 计算动态止损失败: %v", err)
		return false, true
	}

	if hasPrevStop && floatsAlmostEqual(newStopLoss, prevStop) {
		log.Printf("      ⏭️  动态止损未变化，保持 %.4f", newStopLoss)
		return false, true
	}

	log.Printf("      ✏️  %s", reason)

	log.Printf("      🔍 验证止损价格有效性...")
	allowInitialStop := !hasPrevStop && floatsAlmostEqual(newStopLoss, riskInfo.InitialStop)
	isValid, triggerClose := m.isStopLossValid(pos.Side, pos.EntryPrice, newStopLoss, pos.MarkPrice, allowInitialStop)
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
	if err := m.updateStopLoss(pos.Symbol, pos.Side, pos.Quantity, newStopLoss, pos.MarkPrice, reason, prevStop, hasPrevStop); err != nil {
		log.Printf("      ❌ 设置止损单失败: %v", err)
		return false, false
	}

	log.Printf("      ✅ 成功设置动态追踪止损至 %.4f", newStopLoss)
	return true, false
}

// isStopLossValid 验证止损价是否有效，并返回是否需要立即触发紧急平仓
// allowInitialStop 表示当前更新是为了恢复初始风险位（交易所里没有止损单），此时允许止损回到入场价以外
func (m *TrailingStopMonitor) isStopLossValid(side string, entryPrice, newStopLoss, currentPrice float64, allowInitialStop bool) (bool, bool) {
	log.Printf("         [验证] 止损价: %.4f | 入场价: %.4f | 当前价: %.4f", newStopLoss, entryPrice, currentPrice)

	if side == "long" {
		if allowInitialStop {
			log.Printf("         [验证-多单] 特殊情况：交易所缺少止损，允许恢复到初始防守位 %.4f", newStopLoss)
		} else {
			// 多单止损必须满足：止损价不低于入场价（允许等于开仓价实现保本）
			log.Printf("         [验证-多单] 检查1: 止损价 %.4f ≥ 入场价 %.4f?", newStopLoss, entryPrice)
			if newStopLoss < entryPrice {
				log.Printf("         [验证-多单] ❌ 失败: 止损价 %.4f < 入场价 %.4f（无法保护利润）", newStopLoss, entryPrice)
				return false, false
			}
			log.Printf("         [验证-多单] ✅ 通过: 止损价不低于入场价，可保护利润/保本")
		}

		// 2. 止损价低于当前价（合理性检查）
		log.Printf("         [验证-多单] 检查2: 止损价 %.4f < 当前价 %.4f?", newStopLoss, currentPrice)
		if newStopLoss >= currentPrice {
			log.Printf("         [验证-多单] ❌ 失败: 止损价 %.4f ≥ 当前价 %.4f（会立即触发）", newStopLoss, currentPrice)
			return false, true
		}
		log.Printf("         [验证-多单] ✅ 通过: 止损价低于当前价，合理")

	} else {
		if allowInitialStop {
			log.Printf("         [验证-空单] 特殊情况：交易所缺少止损，允许恢复到初始防守位 %.4f", newStopLoss)
		} else {
			// 空单止损必须满足：止损价不高于入场价（允许等于开仓价实现保本）
			log.Printf("         [验证-空单] 检查1: 止损价 %.4f ≤ 入场价 %.4f?", newStopLoss, entryPrice)
			if newStopLoss > entryPrice {
				log.Printf("         [验证-空单] ❌ 失败: 止损价 %.4f > 入场价 %.4f（无法保护利润）", newStopLoss, entryPrice)
				return false, false
			}
			log.Printf("         [验证-空单] ✅ 通过: 止损价不高于入场价，可保护利润/保本")
		}

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
func (m *TrailingStopMonitor) updateStopLoss(symbol, side string, quantity, newStopLoss, currentPrice float64, reason string, existingStop float64, hasExisting bool) error {
	posKey := composePositionKey(symbol, side)
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

	// 读取交易所当前止损价，避免重复提交
	currentStopLoss := existingStop
	hasStopInfo := hasExisting
	if !hasStopInfo {
		var err error
		currentStopLoss, hasStopInfo, err = m.getCurrentStopLoss(symbol, side)
		if err != nil {
			log.Printf("         [追踪止损] ⚠️ 获取实时止损信息失败: %v（将继续尝试更新）", err)
		}
	}

	if hasStopInfo {
		log.Printf("         [追踪止损] 交易所当前止损价: %.4f", currentStopLoss)
		improved := false
		if side == "long" {
			if newStopLoss > currentStopLoss {
				log.Printf("         [追踪止损] 多单止损上移: %.4f -> %.4f (提升 %.4f)",
					currentStopLoss, newStopLoss, newStopLoss-currentStopLoss)
				improved = true
			} else {
				log.Printf("         [追踪止损] ⏭️  多单新止损 %.4f ≤ 当前委托 %.4f，无需更新",
					newStopLoss, currentStopLoss)
			}
		} else {
			if newStopLoss < currentStopLoss {
				log.Printf("         [追踪止损] 空单止损下移: %.4f -> %.4f (降低 %.4f)",
					currentStopLoss, newStopLoss, currentStopLoss-newStopLoss)
				improved = true
			} else {
				log.Printf("         [追踪止损] ⏭️  空单新止损 %.4f ≥ 当前委托 %.4f，无需更新",
					newStopLoss, currentStopLoss)
			}
		}

		if !improved {
			return nil
		}
	} else {
		log.Printf("         [追踪止损] 交易所暂无止损单，视为首次设置 (%.4f)", newStopLoss)
	}

	log.Printf("         [追踪止损] 调用统一止损更新接口...")
	log.Printf("         [追踪止损] 币种: %s | 方向: %s | 数量: %.4f | 止损价: %.4f",
		symbol, strings.ToUpper(side), quantity, newStopLoss)
	if reason == "" {
		reason = fmt.Sprintf("动态追踪止损: 调整至 %.4f", newStopLoss)
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
	if m.owner == nil {
		return fmt.Errorf("owner 未初始化")
	}
	if err := m.owner.ExecuteStopLoss(d, actionRecord); err != nil {
		log.Printf("         [追踪止损] ❌ 调用统一止损更新接口失败: %v", err)
		return fmt.Errorf("追踪止损更新失败: %w", err)
	}

	m.riskRegistry.recordStopLoss(posKey, newStopLoss)

	log.Printf("         [追踪止损] ✅ 通过统一接口成功设置止损 → %.4f", newStopLoss)
	return nil
}

// getCurrentStopLoss 查询交易所当前的止损单价格（按 symbol + side）
func (m *TrailingStopMonitor) getCurrentStopLoss(symbol, side string) (float64, bool, error) {
	client := m.tradingClient()
	if client == nil {
		return 0, false, fmt.Errorf("trader 未初始化")
	}

	orders, err := client.GetOpenOrders(symbol)
	if err != nil {
		return 0, false, err
	}

	targetSide := strings.ToUpper(side)
	var (
		bestPrice float64
		found     bool
	)

	for _, raw := range orders {
		orderType := strings.ToUpper(fmt.Sprintf("%v", raw["type"]))
		if orderType != "STOP_MARKET" && orderType != "STOP" {
			continue
		}

		if closePosition, _ := raw["closePosition"].(bool); !closePosition {
			continue
		}

		positionSide := strings.ToUpper(fmt.Sprintf("%v", raw["positionSide"]))
		if positionSide == "" || positionSide == "BOTH" {
			positionSide = strings.ToUpper(fmt.Sprintf("%v", raw["side"]))
		}
		if positionSide != targetSide {
			continue
		}

		stopPrice, err := FloatFromAny(raw["stopPrice"])
		if err != nil || stopPrice <= 0 {
			continue
		}

		if !found {
			bestPrice = stopPrice
			found = true
			continue
		}

		if targetSide == "LONG" {
			if stopPrice > bestPrice {
				bestPrice = stopPrice
			}
		} else {
			if stopPrice < bestPrice {
				bestPrice = stopPrice
			}
		}
	}

	return bestPrice, found, nil
}

// executeMarketClose 执行紧急市价平仓（止损触发时使用）
func (m *TrailingStopMonitor) executeMarketClose(symbol, side string, currentPrice float64) error {
	log.Printf("         [紧急平仓] 开始执行市价平仓: %s %s (当前价: %.4f)", symbol, strings.ToUpper(side), currentPrice)

	client := m.tradingClient()
	if client == nil {
		return fmt.Errorf("交易接口未初始化")
	}

	var order map[string]interface{}
	var err error

	// 执行平仓
	if side == "long" {
		order, err = client.CloseLong(symbol, 0) // 0 = 全部平仓
		if err != nil {
			return fmt.Errorf("平多仓失败: %w", err)
		}
		log.Printf("         [紧急平仓] 平多仓成功，订单ID: %v", order["orderId"])
	} else {
		order, err = client.CloseShort(symbol, 0) // 0 = 全部平仓
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
	if recorder := m.owner.DecisionRecorder(); recorder != nil {
		if err := recorder.LogDecision(record); err != nil {
			log.Printf("         [紧急平仓] ⚠️  保存决策记录失败: %v", err)
		}
	}

	log.Printf("         [紧急平仓] ✅ 完成: %s %s 已市价平仓", symbol, strings.ToUpper(side))
	return nil
}

// ClearPosition 清除持仓缓存（平仓后调用）
func (m *TrailingStopMonitor) ClearPosition(symbol, side string) {
	if m == nil || m.riskRegistry == nil {
		return
	}

	key := composePositionKey(symbol, side)
	if initialStop, cleared := m.riskRegistry.clear(symbol, side); cleared {
		log.Printf("🧹 [追踪止损] 清除 %s 风险分段缓存 (初始止损: %.4f)", key, initialStop)
	}
}
