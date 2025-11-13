package trader

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type positionSnapshot struct {
	Symbol     string
	Side       string
	EntryPrice float64
	MarkPrice  float64
	Quantity   float64
	Leverage   int
}

func (p positionSnapshot) key() string {
	return p.Symbol + "_" + p.Side
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
