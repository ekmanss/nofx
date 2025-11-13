package trader

import (
	"fmt"
	"log"
	"sync"
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

// RegisterInitialStop 将开仓时的初始止损透传给真实监控器
func (m *SharedTrailingStopMonitor) RegisterInitialStop(symbol, side string, stop float64) {
	if m == nil || m.entry == nil {
		return
	}
	m.entry.monitor.RegisterInitialStop(symbol, side, stop)
}
