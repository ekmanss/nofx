package market

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	dailyKlinesLimit    = 500
	fourHourKlinesLimit = 500
	oneHourKlinesLimit  = 500
	macdSignalPeriod    = 9
)

// getKlinesWithLimit 获取指定数量的K线数据
func getKlinesWithLimit(symbol string, interval string, limit int) ([]Kline, error) {
	apiClient := NewAPIClient()

	// 优先尝试用缓存，但缓存长度不足时直接走API获取完整数量
	if WSMonitorCli != nil {
		allKlines, err := WSMonitorCli.GetCurrentKlines(symbol, interval)
		if err == nil {
			if len(allKlines) >= limit {
				return allKlines[len(allKlines)-limit:], nil
			}
			// 缓存不足指定数量，改为从API获取足量数据
		}
	}

	// 直接从API获取指定数量
	return apiClient.GetKlines(symbol, interval, limit)
}

// Get 获取指定代币的市场数据（仅保留日线所需字段）
func Get(symbol string) (*Data, error) {
	// 标准化 symbol
	symbol = Normalize(symbol)

	apiClient := NewAPIClient()

	// 获取日线K线数据
	klines1d, err := getKlinesWithLimit(symbol, "1d", dailyKlinesLimit)
	if err != nil {
		return nil, fmt.Errorf("获取1天K线失败: %v", err)
	}
	if len(klines1d) == 0 {
		return nil, fmt.Errorf("1天K线数据为空")
	}
	if len(klines1d) > dailyKlinesLimit {
		klines1d = klines1d[len(klines1d)-dailyKlinesLimit:]
	}

	// 获取4小时K线数据
	klines4h, err := getKlinesWithLimit(symbol, "4h", fourHourKlinesLimit)
	if err != nil {
		return nil, fmt.Errorf("获取4小时K线失败: %v", err)
	}
	if len(klines4h) == 0 {
		return nil, fmt.Errorf("4小时K线数据为空")
	}
	if len(klines4h) > fourHourKlinesLimit {
		klines4h = klines4h[len(klines4h)-fourHourKlinesLimit:]
	}

	// 获取1小时K线数据
	klines1h, err := getKlinesWithLimit(symbol, "1h", oneHourKlinesLimit)
	if err != nil {
		return nil, fmt.Errorf("获取1小时K线失败: %v", err)
	}
	if len(klines1h) == 0 {
		return nil, fmt.Errorf("1小时K线数据为空")
	}
	if len(klines1h) > oneHourKlinesLimit {
		klines1h = klines1h[len(klines1h)-oneHourKlinesLimit:]
	}

	// 打印获取到的K线数量
	log.Printf("📊 %s K线数据: 1d=%d条, 4h=%d条, 1h=%d条", symbol, len(klines1d), len(klines4h), len(klines1h))

	// 实时价格：使用4小时最新收盘价
	currentPrice := klines4h[len(klines4h)-1].Close

	// 获取资金费率历史（最近20条）
	fundingRates, err := apiClient.GetFundingRateHistory(symbol, 20)
	if err != nil {
		log.Printf("⚠️  获取资金费率历史失败: %v", err)
	}

	indicators := buildDailyIndicators(klines1d)
	fourHourIndicators := buildFourHourIndicators(klines4h)
	oneHourIndicators := buildOneHourIndicators(klines1h)

	return &Data{
		Symbol:       symbol,
		CurrentPrice: currentPrice,
		Daily: &DailyData{
			Klines:     klines1d,
			Indicators: indicators,
		},
		FourHour: &FourHourData{
			Klines:     klines4h,
			Indicators: fourHourIndicators,
		},
		OneHour: &OneHourData{
			Klines:     klines1h,
			Indicators: oneHourIndicators,
		},
		FundingRates: fundingRates,
	}, nil
}

// buildDailyIndicators 生成日线指标
func buildDailyIndicators(klines []Kline) DailyIndicators {
	sma50 := calculateSMASeries(klines, 50)
	sma200 := calculateSMASeries(klines, 200)
	ema20 := calculateEMASeries(klines, 20)

	macdLine, macdSignal, macdHist := calculateMACDSeries(klines)
	rsi14 := calculateRSISeries(klines, 14)
	atr14 := calculateATRSeries(klines, 14)

	return DailyIndicators{
		SMA50:      sma50,
		SMA200:     sma200,
		EMA20:      ema20,
		MACDLine:   takeLastN(macdLine, 60),
		MACDSignal: takeLastN(macdSignal, 60),
		MACDHist:   takeLastN(macdHist, 60),
		RSI14:      takeLastN(rsi14, 60),
		ATR14:      takeLastN(atr14, 60),
	}
}

// buildFourHourIndicators 生成4小时指标
func buildFourHourIndicators(klines []Kline) FourHourIndicators {
	ema20 := calculateEMASeries(klines, 20)
	ema50 := calculateEMASeries(klines, 50)
	ema100 := calculateEMASeries(klines, 100)
	ema200 := calculateEMASeries(klines, 200)

	macdLine, macdSignal, macdHist := calculateMACDSeries(klines)
	rsi14 := calculateRSISeries(klines, 14)
	atr14 := calculateATRSeries(klines, 14)
	adx14, plusDI14, minusDI14 := calculateADXSeries(klines, 14)
	bollUpper, bollMiddle, bollLower := calculateBollingerBands(klines, 20, 2)

	return FourHourIndicators{
		EMA20:          ema20,
		EMA50:          ema50,
		EMA100:         ema100,
		EMA200:         ema200,
		MACDLine:       takeLastN(macdLine, 60),
		MACDSignal:     takeLastN(macdSignal, 60),
		MACDHist:       takeLastN(macdHist, 60),
		RSI14:          takeLastN(rsi14, 60),
		ATR14:          takeLastN(atr14, 60),
		ADX14:          takeLastN(adx14, 60),
		PlusDI14:       takeLastN(plusDI14, 60),
		MinusDI14:      takeLastN(minusDI14, 60),
		BollUpper20_2:  takeLastN(bollUpper, 60),
		BollMiddle20_2: takeLastN(bollMiddle, 60),
		BollLower20_2:  takeLastN(bollLower, 60),
	}
}

// buildOneHourIndicators 生成1小时指标
func buildOneHourIndicators(klines []Kline) OneHourIndicators {
	ema20 := calculateEMASeries(klines, 20)
	ema50 := calculateEMASeries(klines, 50)

	rsi7 := calculateRSISeries(klines, 7)
	rsi14 := calculateRSISeries(klines, 14)
	bollUpper, bollMiddle, bollLower := calculateBollingerBands(klines, 20, 2)

	return OneHourIndicators{
		EMA20:          ema20,
		EMA50:          ema50,
		RSI7:           takeLastN(rsi7, 60),
		RSI14:          takeLastN(rsi14, 60),
		BollUpper20_2:  takeLastN(bollUpper, 60),
		BollMiddle20_2: takeLastN(bollMiddle, 60),
		BollLower20_2:  takeLastN(bollLower, 60),
	}
}

// calculateSMASeries 计算 SMA 序列（长度与 K线一致，数据不足时填 0）
func calculateSMASeries(klines []Kline, period int) []float64 {
	res := make([]float64, len(klines))
	if len(klines) < period || period <= 0 {
		return res
	}

	sum := 0.0
	for i := 0; i < period; i++ {
		sum += klines[i].Close
	}
	res[period-1] = sum / float64(period)

	for i := period; i < len(klines); i++ {
		sum += klines[i].Close - klines[i-period].Close
		res[i] = sum / float64(period)
	}

	return res
}

// calculateEMASeries 计算 EMA 序列（长度与 K线一致，数据不足时填 0）
func calculateEMASeries(klines []Kline, period int) []float64 {
	res := make([]float64, len(klines))
	if len(klines) < period || period <= 0 {
		return res
	}

	sum := 0.0
	for i := 0; i < period; i++ {
		sum += klines[i].Close
	}
	ema := sum / float64(period)
	res[period-1] = ema

	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(klines); i++ {
		ema = (klines[i].Close-ema)*multiplier + ema
		res[i] = ema
	}

	return res
}

// calculateMACDSeries 计算 MACD（12,26,9），返回 line/signal/hist 全量序列
func calculateMACDSeries(klines []Kline) (line, signal, hist []float64) {
	n := len(klines)
	line = make([]float64, n)
	signal = make([]float64, n)
	hist = make([]float64, n)
	if n == 0 {
		return
	}

	ema12 := calculateEMASeries(klines, 12)
	ema26 := calculateEMASeries(klines, 26)

	var (
		signalEMA   float64
		signalReady bool
		buffer      []float64
	)
	multiplier := 2.0 / float64(macdSignalPeriod+1)

	for i := 0; i < n; i++ {
		if ema12[i] == 0 || ema26[i] == 0 {
			continue
		}

		line[i] = ema12[i] - ema26[i]

		if !signalReady {
			buffer = append(buffer, line[i])
			if len(buffer) == macdSignalPeriod {
				sum := 0.0
				for _, v := range buffer {
					sum += v
				}
				signalEMA = sum / float64(macdSignalPeriod)
				signalReady = true
				signal[i] = signalEMA
				hist[i] = line[i] - signalEMA
			}
			continue
		}

		signalEMA = (line[i]-signalEMA)*multiplier + signalEMA
		signal[i] = signalEMA
		hist[i] = line[i] - signalEMA
	}

	return
}

// calculateRSISeries 计算 RSI 序列（Wilder 平滑）
func calculateRSISeries(klines []Kline, period int) []float64 {
	rsi := make([]float64, len(klines))
	if len(klines) <= period || period <= 0 {
		return rsi
	}

	gain := 0.0
	loss := 0.0
	for i := 1; i <= period; i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			gain += change
		} else {
			loss -= change
		}
	}
	avgGain := gain / float64(period)
	avgLoss := loss / float64(period)

	if avgLoss == 0 {
		rsi[period] = 100
	} else {
		rs := avgGain / avgLoss
		rsi[period] = 100 - (100 / (1 + rs))
	}

	for i := period + 1; i < len(klines); i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) - change) / float64(period)
		}

		if avgLoss == 0 {
			rsi[i] = 100
			continue
		}
		rs := avgGain / avgLoss
		rsi[i] = 100 - (100 / (1 + rs))
	}

	return rsi
}

// calculateATRSeries 计算 ATR 序列
func calculateATRSeries(klines []Kline, period int) []float64 {
	atr := make([]float64, len(klines))
	if len(klines) <= period || period <= 0 {
		return atr
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
	atr[period] = sum / float64(period)

	for i := period + 1; i < len(klines); i++ {
		atr[i] = (atr[i-1]*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}

// calculateADXSeries 计算 ADX 以及 +DI/-DI 序列
func calculateADXSeries(klines []Kline, period int) (adx, plusDI, minusDI []float64) {
	n := len(klines)
	adx = make([]float64, n)
	plusDI = make([]float64, n)
	minusDI = make([]float64, n)
	if n <= period || period <= 0 {
		return
	}

	tr := make([]float64, n)
	plusDM := make([]float64, n)
	minusDM := make([]float64, n)

	for i := 1; i < n; i++ {
		highDiff := klines[i].High - klines[i-1].High
		lowDiff := klines[i-1].Low - klines[i].Low

		if highDiff > 0 && highDiff > lowDiff {
			plusDM[i] = highDiff
		}
		if lowDiff > 0 && lowDiff > highDiff {
			minusDM[i] = lowDiff
		}

		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close
		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)
		tr[i] = math.Max(tr1, math.Max(tr2, tr3))
	}

	trSmoothed := make([]float64, n)
	plusDMSmoothed := make([]float64, n)
	minusDMSmoothed := make([]float64, n)

	sumTR := 0.0
	sumPlusDM := 0.0
	sumMinusDM := 0.0
	for i := 1; i <= period; i++ {
		sumTR += tr[i]
		sumPlusDM += plusDM[i]
		sumMinusDM += minusDM[i]
	}
	trSmoothed[period] = sumTR
	plusDMSmoothed[period] = sumPlusDM
	minusDMSmoothed[period] = sumMinusDM

	for i := period + 1; i < n; i++ {
		trSmoothed[i] = trSmoothed[i-1] - (trSmoothed[i-1] / float64(period)) + tr[i]
		plusDMSmoothed[i] = plusDMSmoothed[i-1] - (plusDMSmoothed[i-1] / float64(period)) + plusDM[i]
		minusDMSmoothed[i] = minusDMSmoothed[i-1] - (minusDMSmoothed[i-1] / float64(period)) + minusDM[i]
	}

	for i := period; i < n; i++ {
		if trSmoothed[i] == 0 {
			continue
		}
		plusDI[i] = 100 * (plusDMSmoothed[i] / trSmoothed[i])
		minusDI[i] = 100 * (minusDMSmoothed[i] / trSmoothed[i])
		diff := math.Abs(plusDI[i] - minusDI[i])
		sum := plusDI[i] + minusDI[i]
		if sum == 0 {
			continue
		}
		dx := 100 * (diff / sum)
		if i == period {
			adx[i] = dx
		} else {
			adx[i] = (adx[i-1]*float64(period-1) + dx) / float64(period)
		}
	}

	return
}

// calculateBollingerBands 计算布林带
func calculateBollingerBands(klines []Kline, period int, multiplier float64) (upper, middle, lower []float64) {
	n := len(klines)
	upper = make([]float64, n)
	middle = make([]float64, n)
	lower = make([]float64, n)
	if n < period || period <= 0 {
		return
	}

	sma := calculateSMASeries(klines, period)
	for i := period - 1; i < n; i++ {
		middle[i] = sma[i]
		sum := 0.0
		for j := i - period + 1; j <= i; j++ {
			diff := klines[j].Close - middle[i]
			sum += diff * diff
		}
		variance := sum / float64(period)
		stdDev := math.Sqrt(variance)
		upper[i] = middle[i] + multiplier*stdDev
		lower[i] = middle[i] - multiplier*stdDev
	}

	return
}

func takeLastN(values []float64, n int) []float64 {
	if len(values) <= n {
		return values
	}
	return append([]float64{}, values[len(values)-n:]...)
}

func takeLastKlines(klines []Kline, n int) []Kline {
	if len(klines) <= n {
		return klines
	}
	return klines[len(klines)-n:]
}

// Format 格式化输出市场数据（按需求输出1d/4h/1h指标和K线）
func Format(data *Data) string {
	var sb strings.Builder

	priceStr := formatPriceWithDynamicPrecision(data.CurrentPrice)
	sb.WriteString(fmt.Sprintf("symbol = %s, current_price = %s\n\n", data.Symbol, priceStr))

	if data.Daily != nil {
		dailyKlines := takeLastKlines(data.Daily.Klines, 200)
		sb.WriteString(fmt.Sprintf("1d ohlcv (latest %d):\n", len(dailyKlines)))
		sb.WriteString(formatKlines(dailyKlines))
		sb.WriteString("\n")

		ind := data.Daily.Indicators
		sb.WriteString("1d Indicators:\n")
		sb.WriteString(fmt.Sprintf("SMA50 (per bar): %s\n", formatFloatSlice(ind.SMA50)))
		sb.WriteString(fmt.Sprintf("SMA200 (per bar): %s\n", formatFloatSlice(ind.SMA200)))
		sb.WriteString(fmt.Sprintf("EMA20 (per bar): %s\n", formatFloatSlice(ind.EMA20)))
		sb.WriteString(fmt.Sprintf("MACD12-26-9 (last %d): line %s | signal %s | hist %s\n",
			len(ind.MACDLine),
			formatFloatSlice(ind.MACDLine),
			formatFloatSlice(ind.MACDSignal),
			formatFloatSlice(ind.MACDHist)))
		sb.WriteString(fmt.Sprintf("RSI14 (last %d): %s\n", len(ind.RSI14), formatFloatSlice(ind.RSI14)))
		sb.WriteString(fmt.Sprintf("ATR14 (last %d): %s\n", len(ind.ATR14), formatFloatSlice(ind.ATR14)))
		sb.WriteString("\n")
	}

	if data.FourHour != nil {
		fourHKlines := takeLastKlines(data.FourHour.Klines, 200)
		sb.WriteString(fmt.Sprintf("4h ohlcv (latest %d):\n", len(fourHKlines)))
		sb.WriteString(formatKlines(fourHKlines))
		sb.WriteString("\n")

		ind := data.FourHour.Indicators
		sb.WriteString("4h Indicators:\n")
		sb.WriteString(fmt.Sprintf("EMA20/50/100/200 (per bar): %s | %s | %s | %s\n",
			formatFloatSlice(ind.EMA20),
			formatFloatSlice(ind.EMA50),
			formatFloatSlice(ind.EMA100),
			formatFloatSlice(ind.EMA200)))
		sb.WriteString(fmt.Sprintf("MACD12-26-9 (last %d): line %s | signal %s | hist %s\n",
			len(ind.MACDLine),
			formatFloatSlice(ind.MACDLine),
			formatFloatSlice(ind.MACDSignal),
			formatFloatSlice(ind.MACDHist)))
		sb.WriteString(fmt.Sprintf("RSI14 (last %d): %s\n", len(ind.RSI14), formatFloatSlice(ind.RSI14)))
		sb.WriteString(fmt.Sprintf("ATR14 (last %d): %s\n", len(ind.ATR14), formatFloatSlice(ind.ATR14)))
		sb.WriteString(fmt.Sprintf("ADX14 (+DI/-DI) (last %d): adx %s | +di %s | -di %s\n",
			len(ind.ADX14),
			formatFloatSlice(ind.ADX14),
			formatFloatSlice(ind.PlusDI14),
			formatFloatSlice(ind.MinusDI14)))
		sb.WriteString(fmt.Sprintf("Bollinger Bands 20,2 (last %d): upper %s | middle %s | lower %s\n",
			len(ind.BollUpper20_2),
			formatFloatSlice(ind.BollUpper20_2),
			formatFloatSlice(ind.BollMiddle20_2),
			formatFloatSlice(ind.BollLower20_2)))
		sb.WriteString("\n")
	}

	if data.OneHour != nil {
		oneHKlines := takeLastKlines(data.OneHour.Klines, 200)
		sb.WriteString(fmt.Sprintf("1h ohlcv (latest %d):\n", len(oneHKlines)))
		sb.WriteString(formatKlines(oneHKlines))
		sb.WriteString("\n")

		ind := data.OneHour.Indicators
		sb.WriteString("1h Indicators:\n")
		sb.WriteString(fmt.Sprintf("EMA20/50 (per bar): %s | %s\n",
			formatFloatSlice(ind.EMA20),
			formatFloatSlice(ind.EMA50)))
		sb.WriteString(fmt.Sprintf("RSI7 (last %d): %s\n", len(ind.RSI7), formatFloatSlice(ind.RSI7)))
		sb.WriteString(fmt.Sprintf("RSI14 (last %d): %s\n", len(ind.RSI14), formatFloatSlice(ind.RSI14)))
		sb.WriteString(fmt.Sprintf("Bollinger Bands 20,2 (last %d): upper %s | middle %s | lower %s\n",
			len(ind.BollUpper20_2),
			formatFloatSlice(ind.BollUpper20_2),
			formatFloatSlice(ind.BollMiddle20_2),
			formatFloatSlice(ind.BollLower20_2)))
		sb.WriteString("\n")
	}

	if len(data.FundingRates) > 0 {
		sb.WriteString(fmt.Sprintf("Funding rate history (last %d):\n", len(data.FundingRates)))
		sb.WriteString(formatFundingRates(data.FundingRates))
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatPriceWithDynamicPrecision 根据价格区间动态选择精度
// 这样可以完美支持从超低价 meme coin (< 0.0001) 到 BTC/ETH 的所有币种
func formatPriceWithDynamicPrecision(price float64) string {
	switch {
	case price < 0.0001:
		return fmt.Sprintf("%.8f", price)
	case price < 0.001:
		return fmt.Sprintf("%.6f", price)
	case price < 0.01:
		return fmt.Sprintf("%.6f", price)
	case price < 1.0:
		return fmt.Sprintf("%.4f", price)
	case price < 100:
		return fmt.Sprintf("%.4f", price)
	default:
		return fmt.Sprintf("%.2f", price)
	}
}

// formatFloatSlice 格式化float64切片为字符串（使用动态精度）
func formatFloatSlice(values []float64) string {
	if len(values) == 0 {
		return "[]"
	}
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = formatPriceWithDynamicPrecision(v)
	}
	return "[" + strings.Join(strValues, ", ") + "]"
}

// formatKlines 格式化K线数据为字符串
func formatKlines(klines []Kline) string {
	var sb strings.Builder
	// UTC+8 timezone
	utc8 := time.FixedZone("UTC+8", 8*60*60)
	for i, k := range klines {
		openTime := time.Unix(k.OpenTime/1000, (k.OpenTime%1000)*1000000).In(utc8)
		sb.WriteString(fmt.Sprintf("  [%d] OpenTime: %s, O: %.2f, H: %.2f, L: %.2f, C: %.2f, V: %.2f\n",
			i+1, openTime.Format("2006-01-02 15:04:05"), k.Open, k.High, k.Low, k.Close, k.Volume))
	}
	return sb.String()
}

func formatFundingRates(rates []FundingRate) string {
	var sb strings.Builder
	utc8 := time.FixedZone("UTC+8", 8*60*60)
	start := 0
	if len(rates) > 20 {
		start = len(rates) - 20
	}
	for i := start; i < len(rates); i++ {
		ts := time.UnixMilli(rates[i].FundingTime).In(utc8)
		sb.WriteString(fmt.Sprintf("  [%d] %s rate: %.6f, mark: %.4f\n", i+1, ts.Format("2006-01-02 15:04:05"), rates[i].FundingRate, rates[i].MarkPrice))
	}
	return sb.String()
}

// Normalize 标准化symbol,确保是USDT交易对
func Normalize(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if strings.HasSuffix(symbol, "USDT") {
		return symbol
	}
	return symbol + "USDT"
}

// isStaleData detects stale data (consecutive price freeze)
// Fix DOGEUSDT-style issue: consecutive N periods with completely unchanged prices indicate data source anomaly
func isStaleData(klines []Kline, symbol string) bool {
	if len(klines) < 5 {
		return false
	}

	const stalePriceThreshold = 5
	const priceTolerancePct = 0.0001

	recentKlines := klines[len(klines)-stalePriceThreshold:]
	firstPrice := recentKlines[0].Close

	for i := 1; i < len(recentKlines); i++ {
		priceDiff := math.Abs(recentKlines[i].Close-firstPrice) / firstPrice
		if priceDiff > priceTolerancePct {
			return false
		}
	}

	allVolumeZero := true
	for _, k := range recentKlines {
		if k.Volume > 0 {
			allVolumeZero = false
			break
		}
	}

	if allVolumeZero {
		log.Printf("⚠️  %s stale data confirmed: price freeze + zero volume", symbol)
		return true
	}

	log.Printf("⚠️  %s detected extreme price stability (no fluctuation for %d consecutive periods), but volume is normal", symbol, stalePriceThreshold)
	return false
}

// parseFloat 解析float值
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case string:
		return strconv.ParseFloat(val, 64)
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}
