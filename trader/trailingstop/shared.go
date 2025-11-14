package trailingstop

import (
	"log"
	"sync"
)

// Manager keeps one trailing stop monitor per unique account key and shares it among traders.
type Manager struct {
	factory MonitorFactory

	mu      sync.Mutex
	entries map[string]*sharedEntry
}

type sharedEntry struct {
	monitor Monitor
	owners  map[string]Owner
}

// SharedMonitor represents a handle to the shared monitor for a specific account.
type SharedMonitor struct {
	manager    *Manager
	accountKey string
	ownerID    string
	entry      *sharedEntry
}

// NewManager builds a manager with the provided monitor factory.
func NewManager(factory MonitorFactory) *Manager {
	return &Manager{
		factory: factory,
		entries: make(map[string]*sharedEntry),
	}
}

// Acquire returns a handle to the shared monitor for the owner's account key.
func (m *Manager) Acquire(owner Owner) *SharedMonitor {
	if m == nil || owner == nil {
		return nil
	}
	accountKey := owner.AccountKey()
	if accountKey == "" {
		log.Printf("⚠️  [追踪止损] Owner %s account key 为空，无法创建共享监控器", owner.TraderName())
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.entries[accountKey]
	if !exists {
		monitor := m.factory(owner)
		if monitor == nil {
			log.Printf("⚠️  [追踪止损] 无法创建监控器: %s", owner.TraderName())
			return nil
		}
		entry = &sharedEntry{
			monitor: monitor,
			owners:  make(map[string]Owner),
		}
		m.entries[accountKey] = entry
		log.Printf("🆕 [追踪止损] 创建账户监控器: %s (首个交易员: %s)", maskAccountKey(accountKey), owner.TraderName())
	} else {
		log.Printf("♻️ [追踪止损] 复用账户监控器: %s (新增交易员: %s)", maskAccountKey(accountKey), owner.TraderName())
	}

	entry.owners[owner.TraderID()] = owner
	entry.monitor.SetOwner(owner)

	return &SharedMonitor{
		manager:    m,
		accountKey: accountKey,
		ownerID:    owner.TraderID(),
		entry:      entry,
	}
}

// Start bootstraps the underlying monitor if necessary.
func (s *SharedMonitor) Start() {
	if s == nil || s.entry == nil {
		return
	}
	s.entry.monitor.Start()
}

// Stop releases the current owner's reference to the shared monitor.
func (s *SharedMonitor) Stop() {
	if s == nil || s.entry == nil || s.manager == nil {
		return
	}

	var (
		monitorToStop Monitor
		nextOwner     Owner
		remaining     int
	)

	s.manager.mu.Lock()
	if entry, exists := s.manager.entries[s.accountKey]; exists && entry == s.entry {
		delete(entry.owners, s.ownerID)
		remaining = len(entry.owners)
		if remaining == 0 {
			delete(s.manager.entries, s.accountKey)
			monitorToStop = entry.monitor
		} else {
			for _, candidate := range entry.owners {
				nextOwner = candidate
				break
			}
		}
	}
	s.manager.mu.Unlock()

	if monitorToStop != nil {
		monitorToStop.Stop()
		log.Printf("🛑 [追踪止损] 关闭账户监控器: %s（无活跃交易员）", maskAccountKey(s.accountKey))
	} else if nextOwner != nil {
		s.entry.monitor.SetOwner(nextOwner)
		log.Printf("👑 [追踪止损] 切换监控器负责人 → %s (账户: %s)", nextOwner.TraderName(), maskAccountKey(s.accountKey))
	}

	s.entry = nil
}

// ClearPosition proxies the cleanup call to the shared monitor.
func (s *SharedMonitor) ClearPosition(symbol, side string) {
	if s == nil || s.entry == nil {
		return
	}
	s.entry.monitor.ClearPosition(symbol, side)
}

// RegisterInitialStop proxies the initial stop registration to the shared monitor.
func (s *SharedMonitor) RegisterInitialStop(symbol, side string, stop float64) {
	if s == nil || s.entry == nil {
		return
	}
	s.entry.monitor.RegisterInitialStop(symbol, side, stop)
}

func maskAccountKey(key string) string {
	if len(key) <= 8 {
		return key
	}
	return key[:4] + "..." + key[len(key)-4:]
}
