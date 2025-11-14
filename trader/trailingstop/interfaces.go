package trailingstop

import (
	"nofx/decision"
	"nofx/logger"
)

// TradingClient describes the minimum API the trailing stop monitor needs from any exchange client.
// It mirrors the subset of methods from trader.Trader so we can decouple this package from the main trader package.
type TradingClient interface {
	GetPositions() ([]map[string]interface{}, error)
	GetOpenOrders(symbol string) ([]map[string]interface{}, error)
	CloseLong(symbol string, quantity float64) (map[string]interface{}, error)
	CloseShort(symbol string, quantity float64) (map[string]interface{}, error)
}

// DecisionRecorder captures the minimal logging capability required to store trailing stop decisions.
type DecisionRecorder interface {
	LogDecision(record *logger.DecisionRecord) error
}

// Owner provides all context the trailing stop monitor needs from its host auto-trader.
type Owner interface {
	TraderID() string
	TraderName() string
	AccountKey() string
	TradingClient() TradingClient
	ExecuteStopLoss(decision *decision.Decision, action *logger.DecisionAction) error
	DecisionRecorder() DecisionRecorder
}

// Monitor exposes the operations required by the shared monitor manager.
type Monitor interface {
	Start()
	Stop()
	SetOwner(owner Owner)
	ClearPosition(symbol, side string)
	RegisterInitialStop(symbol, side string, stop float64)
}

// MonitorFactory builds a monitor instance for the supplied owner.
type MonitorFactory func(owner Owner) Monitor
