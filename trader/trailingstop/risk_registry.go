package trailingstop

import (
	"strings"
	"sync"
	"time"
)

type riskStageInfo struct {
	InitialStop float64

	PeakPrice float64
	MaxR      float64

	LastRecordedStop float64
	HasRecordedStop  bool

	OpenedAt time.Time
}

type riskStateRemoval struct {
	key         string
	initialStop float64
}

type riskRegistry struct {
	mu     sync.RWMutex
	states map[string]*riskStageInfo
}

func newRiskRegistry() *riskRegistry {
	return &riskRegistry{states: make(map[string]*riskStageInfo)}
}

func (r *riskRegistry) registerInitialStop(symbol, side string, stop float64) string {
	if r == nil {
		return ""
	}
	key := composePositionKey(symbol, side)
	now := time.Now()
	r.mu.Lock()
	r.states[key] = &riskStageInfo{
		InitialStop: stop,
		OpenedAt:    now,
	}
	r.mu.Unlock()
	return key
}

func (r *riskRegistry) snapshot(key string) (*riskStageInfo, bool) {
	if r == nil {
		return nil, false
	}

	r.mu.RLock()
	info, ok := r.states[key]
	if !ok || info == nil {
		r.mu.RUnlock()
		return nil, false
	}
	copied := *info
	r.mu.RUnlock()
	return &copied, true
}

func (r *riskRegistry) recordStopLoss(key string, stop float64) {
	if r == nil || stop <= 0 {
		return
	}

	r.mu.Lock()
	if entry, ok := r.states[key]; ok && entry != nil {
		entry.LastRecordedStop = stop
		entry.HasRecordedStop = true
	}
	r.mu.Unlock()
}

func (r *riskRegistry) updatePeakAndMaxR(pos *Snapshot, key string, currentR float64) {
	if r == nil || pos == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	info, ok := r.states[key]
	if !ok || info == nil {
		return
	}

	if info.OpenedAt.IsZero() {
		info.OpenedAt = time.Now()
	}

	price := pos.MarkPrice
	if info.PeakPrice == 0 {
		info.PeakPrice = price
	}

	if pos.Side == "long" {
		if price > info.PeakPrice {
			info.PeakPrice = price
		}
	} else {
		if price < info.PeakPrice {
			info.PeakPrice = price
		}
	}

	if currentR > info.MaxR {
		info.MaxR = currentR
	}
}

func (r *riskRegistry) cleanup(activeKeys map[string]struct{}) []riskStateRemoval {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.states) == 0 {
		return nil
	}

	shouldKeep := func(key string) bool {
		if len(activeKeys) == 0 {
			return false
		}
		_, ok := activeKeys[key]
		return ok
	}

	var removed []riskStateRemoval
	for key, info := range r.states {
		if shouldKeep(key) {
			continue
		}
		removed = append(removed, riskStateRemoval{key: key, initialStop: info.InitialStop})
		delete(r.states, key)
	}

	return removed
}

func (r *riskRegistry) clear(symbol, side string) (float64, bool) {
	if r == nil {
		return 0, false
	}
	key := composePositionKey(symbol, side)

	r.mu.Lock()
	defer r.mu.Unlock()

	info, ok := r.states[key]
	if !ok || info == nil {
		return 0, false
	}

	delete(r.states, key)
	return info.InitialStop, true
}

func composePositionKey(symbol, side string) string {
	normalizedSide := strings.ToLower(strings.TrimSpace(side))
	return symbol + "_" + normalizedSide
}
