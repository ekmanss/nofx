package market

import "testing"

// generateDailyKlines 生成简单的日线数据（价格缓慢上升）
func generateDailyKlines(count int) []Kline {
	klines := make([]Kline, count)
	for i := 0; i < count; i++ {
		base := 100.0 + float64(i)*0.5
		klines[i] = Kline{
			OpenTime:  int64(i) * 86_400_000,
			Open:      base,
			High:      base + 1,
			Low:       base - 1,
			Close:     base + 0.3,
			Volume:    1000 + float64(i),
			CloseTime: int64(i+1)*86_400_000 - 1,
		}
	}
	return klines
}

// generate4HKlines 生成4小时级别 K 线
func generate4HKlines(count int) []Kline {
	klines := make([]Kline, count)
	for i := 0; i < count; i++ {
		base := 50.0 + float64(i)*0.3
		klines[i] = Kline{
			OpenTime:  int64(i) * 14_400_000, // 4h in ms
			Open:      base,
			High:      base + 0.8,
			Low:       base - 0.6,
			Close:     base + 0.2,
			Volume:    500 + float64(i),
			CloseTime: int64(i+1)*14_400_000 - 1,
		}
	}
	return klines
}

// generate1HKlines 生成1小时级别 K 线
func generate1HKlines(count int) []Kline {
	klines := make([]Kline, count)
	for i := 0; i < count; i++ {
		base := 20.0 + float64(i)*0.2
		klines[i] = Kline{
			OpenTime:  int64(i) * 3_600_000,
			Open:      base,
			High:      base + 0.5,
			Low:       base - 0.4,
			Close:     base + 0.1,
			Volume:    300 + float64(i),
			CloseTime: int64(i+1)*3_600_000 - 1,
		}
	}
	return klines
}

func TestBuildDailyIndicatorsLengths(t *testing.T) {
	klines := generateDailyKlines(250)
	ind := buildDailyIndicators(klines)

	if len(ind.SMA50) != len(klines) {
		t.Fatalf("SMA50 length = %d, want %d", len(ind.SMA50), len(klines))
	}
	if len(ind.SMA200) != len(klines) {
		t.Fatalf("SMA200 length = %d, want %d", len(ind.SMA200), len(klines))
	}
	if len(ind.EMA20) != len(klines) {
		t.Fatalf("EMA20 length = %d, want %d", len(ind.EMA20), len(klines))
	}
	if len(ind.MACDLine) != 60 || len(ind.MACDSignal) != 60 || len(ind.MACDHist) != 60 {
		t.Fatalf("MACD lengths line/signal/hist = %d/%d/%d, want 60/60/60", len(ind.MACDLine), len(ind.MACDSignal), len(ind.MACDHist))
	}
	if len(ind.RSI14) != 60 {
		t.Fatalf("RSI14 length = %d, want 60", len(ind.RSI14))
	}
	if len(ind.ATR14) != 60 {
		t.Fatalf("ATR14 length = %d, want 60", len(ind.ATR14))
	}

	if ind.SMA50[len(ind.SMA50)-1] == 0 || ind.SMA200[len(ind.SMA200)-1] == 0 || ind.EMA20[len(ind.EMA20)-1] == 0 {
		t.Fatalf("expected moving averages to have non-zero latest values")
	}
	if ind.MACDLine[len(ind.MACDLine)-1] == 0 || ind.RSI14[len(ind.RSI14)-1] == 0 || ind.ATR14[len(ind.ATR14)-1] == 0 {
		t.Fatalf("expected latest MACD/RSI/ATR values to be non-zero")
	}
}

func TestBuildDailyIndicatorsShortSeries(t *testing.T) {
	klines := generateDailyKlines(40)
	ind := buildDailyIndicators(klines)

	if len(ind.SMA50) != len(klines) || len(ind.SMA200) != len(klines) || len(ind.EMA20) != len(klines) {
		t.Fatalf("indicator lengths should match klines length (%d)", len(klines))
	}
	if len(ind.MACDLine) != len(klines) || len(ind.MACDSignal) != len(klines) || len(ind.MACDHist) != len(klines) {
		t.Fatalf("MACD slices should not exceed source length when data不足, got %d", len(ind.MACDLine))
	}
	if len(ind.RSI14) != len(klines) || len(ind.ATR14) != len(klines) {
		t.Fatalf("RSI/ATR slices should not exceed source length when data不足, got %d/%d", len(ind.RSI14), len(ind.ATR14))
	}

	if ind.SMA50[len(ind.SMA50)-1] != 0 {
		t.Fatalf("SMA50 should be zero when period > data length")
	}
	if ind.SMA200[len(ind.SMA200)-1] != 0 {
		t.Fatalf("SMA200 should be zero when period > data length")
	}
}

func TestBuildFourHourIndicatorsLengths(t *testing.T) {
	klines := generate4HKlines(200)
	ind := buildFourHourIndicators(klines)

	if len(ind.EMA20) != len(klines) || len(ind.EMA50) != len(klines) || len(ind.EMA100) != len(klines) || len(ind.EMA200) != len(klines) {
		t.Fatalf("EMA series length mismatch: want %d", len(klines))
	}
	if len(ind.MACDLine) != 60 || len(ind.MACDSignal) != 60 || len(ind.MACDHist) != 60 {
		t.Fatalf("MACD series lengths = %d/%d/%d, want 60", len(ind.MACDLine), len(ind.MACDSignal), len(ind.MACDHist))
	}
	if len(ind.RSI14) != 60 || len(ind.ATR14) != 60 || len(ind.ADX14) != 60 || len(ind.PlusDI14) != 60 || len(ind.MinusDI14) != 60 {
		t.Fatalf("RSI/ATR/ADX/DI lengths incorrect")
	}
	if len(ind.BollUpper20_2) != 60 || len(ind.BollMiddle20_2) != 60 || len(ind.BollLower20_2) != 60 {
		t.Fatalf("Bollinger lengths incorrect")
	}

	lastIdx := len(ind.EMA20) - 1
	if ind.EMA20[lastIdx] == 0 || ind.EMA200[lastIdx] == 0 {
		t.Fatalf("expected EMA values to be non-zero at latest bar")
	}
	if ind.MACDLine[len(ind.MACDLine)-1] == 0 || ind.RSI14[len(ind.RSI14)-1] == 0 || ind.ATR14[len(ind.ATR14)-1] == 0 || ind.ADX14[len(ind.ADX14)-1] == 0 {
		t.Fatalf("expected MACD/RSI/ATR/ADX latest values to be non-zero")
	}
	if ind.BollUpper20_2[len(ind.BollUpper20_2)-1] == 0 || ind.BollMiddle20_2[len(ind.BollMiddle20_2)-1] == 0 || ind.BollLower20_2[len(ind.BollLower20_2)-1] == 0 {
		t.Fatalf("expected Bollinger values to be non-zero")
	}
}

func TestBuildOneHourIndicatorsLengths(t *testing.T) {
	klines := generate1HKlines(200)
	ind := buildOneHourIndicators(klines)

	if len(ind.EMA20) != len(klines) || len(ind.EMA50) != len(klines) {
		t.Fatalf("EMA series length mismatch: want %d", len(klines))
	}
	if len(ind.RSI7) != 60 || len(ind.RSI14) != 60 {
		t.Fatalf("RSI lengths incorrect")
	}
	if len(ind.BollUpper20_2) != 60 || len(ind.BollMiddle20_2) != 60 || len(ind.BollLower20_2) != 60 {
		t.Fatalf("Bollinger lengths incorrect")
	}

	lastIdx := len(ind.EMA20) - 1
	if ind.EMA20[lastIdx] == 0 || ind.EMA50[lastIdx] == 0 {
		t.Fatalf("expected EMA values to be non-zero at latest bar")
	}
	if ind.RSI7[len(ind.RSI7)-1] == 0 || ind.RSI14[len(ind.RSI14)-1] == 0 {
		t.Fatalf("expected RSI latest values to be non-zero")
	}
	if ind.BollUpper20_2[len(ind.BollUpper20_2)-1] == 0 || ind.BollMiddle20_2[len(ind.BollMiddle20_2)-1] == 0 || ind.BollLower20_2[len(ind.BollLower20_2)-1] == 0 {
		t.Fatalf("expected Bollinger values to be non-zero")
	}
}
