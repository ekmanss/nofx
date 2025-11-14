package trailingstop

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const defaultLeverage = 5

// Snapshot captures the essential information about an individual position used by the trailing stop logic.
type Snapshot struct {
	Symbol     string
	Side       string
	EntryPrice float64
	MarkPrice  float64
	Quantity   float64
	Leverage   int
}

// Key returns a stable key for referencing the snapshot inside caches (symbol + side).
func (s Snapshot) Key() string {
	return composePositionKey(s.Symbol, s.Side)
}

// NewSnapshot converts a raw position map (as returned by the exchange adapters) into a strongly typed Snapshot.
// It performs strict validation so downstream logic can assume fields are valid.
func NewSnapshot(raw map[string]interface{}) (*Snapshot, error) {
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

	entryPrice, err := FloatFromAny(raw["entryPrice"])
	if err != nil {
		return nil, fmt.Errorf("%s %s entryPrice 解析失败: %w", symbol, side, err)
	}

	markPrice, err := FloatFromAny(raw["markPrice"])
	if err != nil {
		return nil, fmt.Errorf("%s %s markPrice 解析失败: %w", symbol, side, err)
	}

	quantity, err := FloatFromAny(raw["positionAmt"])
	if err != nil {
		return nil, fmt.Errorf("%s %s positionAmt 解析失败: %w", symbol, side, err)
	}
	quantity = math.Abs(quantity)

	leverage := defaultLeverage
	if lev, err := FloatFromAny(raw["leverage"]); err == nil && lev > 0 {
		leverage = int(math.Round(math.Max(lev, 1)))
	}

	return &Snapshot{
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

// FloatFromAny converts an interface{} to float64 with rich error messages.
func FloatFromAny(value interface{}) (float64, error) {
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
