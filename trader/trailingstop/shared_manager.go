package trailingstop

var sharedMonitorManager = NewManager(func(owner Owner) Monitor {
	return NewTrailingStopMonitor(owner)
})

// AcquireSharedTrailingStopMonitor exposes the shared monitor pool for external callers (e.g. AutoTrader).
func AcquireSharedTrailingStopMonitor(owner Owner) *SharedMonitor {
	return sharedMonitorManager.Acquire(owner)
}
