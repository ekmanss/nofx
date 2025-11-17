package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"nofx/market"
	"os"
	"strings"
	"time"
)

const (
	hourlyLimit       = 60
	dailyLimit        = 20
	weeklyDisplay     = 20
	weeklyFetchLimit  = 80
	monthlyLimit      = 60
	defaultOutputName = "klines_report.txt"
)

type weeklyIndicatorSeries struct {
	EMA20 []float64
	EMA50 []float64
	ATR3  []float64
	ATR14 []float64
	MACD  []float64
	RSI14 []float64
}

func main() {
	symbol := flag.String("symbol", "BTCUSDT", "交易对（例如 BTCUSDT）")
	outPath := flag.String("out", defaultOutputName, "输出 txt 文件路径")
	flag.Parse()

	normalizedSymbol := market.Normalize(*symbol)
	client := market.NewAPIClient()

	hourly, err := client.GetKlines(normalizedSymbol, "1h", hourlyLimit)
	exitOnErr("获取小时线失败", err)

	daily, err := client.GetKlines(normalizedSymbol, "1d", dailyLimit)
	exitOnErr("获取日线失败", err)

	weeklyRaw, err := client.GetKlines(normalizedSymbol, "1w", weeklyFetchLimit)
	exitOnErr("获取周线失败", err)

	if len(weeklyRaw) == 0 {
		log.Fatalf("未获取到周线数据")
	}
	weekly := lastKlines(weeklyRaw, weeklyDisplay)

	monthly, err := client.GetKlines(normalizedSymbol, "1M", monthlyLimit)
	exitOnErr("获取月线失败", err)

	indicators := calculateWeeklyIndicatorSeries(weeklyRaw, len(weekly))

	report := buildReport(normalizedSymbol, hourly, daily, weekly, monthly, indicators)
	if err := os.WriteFile(*outPath, []byte(report), 0o644); err != nil {
		log.Fatalf("写入文件失败: %v", err)
	}

	log.Printf("✅ 数据已写入 %s", *outPath)
}

func exitOnErr(msg string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}

func calculateWeeklyIndicatorSeries(allKlines []market.Kline, outputCount int) weeklyIndicatorSeries {
	outputCount = minInt(outputCount, len(allKlines))
	if outputCount == 0 {
		return weeklyIndicatorSeries{}
	}

	return weeklyIndicatorSeries{
		EMA20: trimFloatSeries(buildSeriesWithPeriod(allKlines, 20, calculateEMA), outputCount),
		EMA50: trimFloatSeries(buildSeriesWithPeriod(allKlines, 50, calculateEMA), outputCount),
		ATR3:  trimFloatSeries(buildSeriesWithPeriod(allKlines, 3, calculateATR), outputCount),
		ATR14: trimFloatSeries(buildSeriesWithPeriod(allKlines, 14, calculateATR), outputCount),
		MACD:  trimFloatSeries(buildMACDSeries(allKlines), outputCount),
		RSI14: trimFloatSeries(buildSeriesWithPeriod(allKlines, 14, calculateRSI), outputCount),
	}
}

func buildReport(symbol string, hourly, daily, weekly, monthly []market.Kline, indi weeklyIndicatorSeries) string {
	var sb strings.Builder
	now := time.Now().In(time.FixedZone("UTC+8", 8*3600))

	sb.WriteString(fmt.Sprintf("Symbol: %s\n生成时间(UTC+8): %s\n\n", symbol, now.Format("2006-01-02 15:04:05")))

	sb.WriteString("=== 周线指标序列 (对应最近20条周线) ===\n")
	sb.WriteString(formatWeeklyIndicators(weekly, indi))
	sb.WriteString("\n")

	sb.WriteString("=== 最近60条小时线 ===\n")
	sb.WriteString(formatKlinesWithTime(hourly))
	sb.WriteString("\n=== 最近20条日线 ===\n")
	sb.WriteString(formatKlines(daily))
	sb.WriteString("\n=== 最近20条周线 ===\n")
	sb.WriteString(formatKlines(weekly))
	sb.WriteString("\n=== 最近60条月线 ===\n")
	sb.WriteString(formatKlines(monthly))

	return sb.String()
}

func formatWeeklyIndicators(weekly []market.Kline, indi weeklyIndicatorSeries) string {
	var sb strings.Builder
	location := time.FixedZone("UTC+8", 8*3600)
	length := len(weekly)
	for i := 0; i < length; i++ {
		openTime := time.UnixMilli(weekly[i].OpenTime).In(location)
		sb.WriteString(fmt.Sprintf(
			"[%02d] %s | EMA20: %.4f EMA50: %.4f ATR3: %.4f ATR14: %.4f MACD: %.4f RSI14: %.2f\n",
			i+1,
			openTime.Format("2006-01-02"),
			valueAt(indi.EMA20, i),
			valueAt(indi.EMA50, i),
			valueAt(indi.ATR3, i),
			valueAt(indi.ATR14, i),
			valueAt(indi.MACD, i),
			valueAt(indi.RSI14, i),
		))
	}
	return sb.String()
}

func formatKlines(klines []market.Kline) string {
	var sb strings.Builder
	location := time.FixedZone("UTC+8", 8*3600)
	for idx, k := range klines {
		openTime := time.UnixMilli(k.OpenTime).In(location)
		sb.WriteString(fmt.Sprintf(
			"[%02d] %s | O: %.4f H: %.4f L: %.4f C: %.4f V: %.4f\n",
			idx+1,
			openTime.Format("2006-01-02"),
			k.Open,
			k.High,
			k.Low,
			k.Close,
			k.Volume,
		))
	}
	return sb.String()
}

func formatKlinesWithTime(klines []market.Kline) string {
	var sb strings.Builder
	location := time.FixedZone("UTC+8", 8*3600)
	for idx, k := range klines {
		openTime := time.UnixMilli(k.OpenTime).In(location)
		sb.WriteString(fmt.Sprintf(
			"[%02d] %s | O: %.4f H: %.4f L: %.4f C: %.4f V: %.4f\n",
			idx+1,
			openTime.Format("2006-01-02 15:04"),
			k.Open,
			k.High,
			k.Low,
			k.Close,
			k.Volume,
		))
	}
	return sb.String()
}

func valueAt(values []float64, idx int) float64 {
	if idx < 0 || idx >= len(values) {
		return 0
	}
	return values[idx]
}

func calculateEMA(klines []market.Kline, period int) float64 {
	if len(klines) < period || period <= 0 {
		return 0
	}

	sum := 0.0
	for i := 0; i < period; i++ {
		sum += klines[i].Close
	}
	ema := sum / float64(period)

	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(klines); i++ {
		ema = (klines[i].Close-ema)*multiplier + ema
	}

	return ema
}

func calculateMACD(klines []market.Kline) float64 {
	if len(klines) < 26 {
		return 0
	}

	ema12 := calculateEMA(klines, 12)
	ema26 := calculateEMA(klines, 26)

	return ema12 - ema26
}

func calculateRSI(klines []market.Kline, period int) float64 {
	if len(klines) <= period || period <= 0 {
		return 0
	}

	gains := 0.0
	losses := 0.0
	for i := 1; i <= period; i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	for i := period + 1; i < len(klines); i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) + (-change)) / float64(period)
		}
	}

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func calculateATR(klines []market.Kline, period int) float64 {
	if len(klines) <= period || period <= 0 {
		return 0
	}

	trs := make([]float64, len(klines))
	for i := 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close

		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)

		trs[i] = math.Max(tr1, math.Max(tr2, tr3))
	}

	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trs[i]
	}
	atr := sum / float64(period)

	for i := period + 1; i < len(klines); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}

type indicatorFunc func([]market.Kline, int) float64

func buildSeriesWithPeriod(klines []market.Kline, period int, calc indicatorFunc) []float64 {
	series := make([]float64, len(klines))
	if period <= 0 {
		return series
	}
	for i := range klines {
		series[i] = calc(klines[:i+1], period)
	}
	return series
}

func buildMACDSeries(klines []market.Kline) []float64 {
	series := make([]float64, len(klines))
	for i := range klines {
		series[i] = calculateMACD(klines[:i+1])
	}
	return series
}

func trimFloatSeries(values []float64, count int) []float64 {
	if count <= 0 || len(values) == 0 {
		return nil
	}
	if len(values) <= count {
		cp := make([]float64, len(values))
		copy(cp, values)
		return cp
	}
	start := len(values) - count
	cp := make([]float64, count)
	copy(cp, values[start:])
	return cp
}

func lastKlines(klines []market.Kline, count int) []market.Kline {
	if count <= 0 || len(klines) == 0 {
		return nil
	}
	if len(klines) <= count {
		return klines
	}
	return klines[len(klines)-count:]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
