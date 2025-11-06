package market

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
)

// ==================== 多时间框架数据获取 ====================

// getMultiTimeframeData 获取多时间框架数据
func getMultiTimeframeData(symbol string) (*MultiTimeframeData, error) {
	data := &MultiTimeframeData{}

	// 获取15分钟数据 (主要交易框架)
	klines15m, err := getKlinesFromAPI(symbol, "15m", 40)
	if err != nil {
		return nil, fmt.Errorf("获取15分钟K线失败: %v", err)
	}
	data.Timeframe15m = calculateTimeframeData(klines15m, "15m")

	// 获取1小时数据 (趋势确认)
	klines1h, err := getKlinesFromAPI(symbol, "1h", 50)
	if err != nil {
		return nil, fmt.Errorf("获取1小时K线失败: %v", err)
	}
	data.Timeframe1h = calculateTimeframeData(klines1h, "1h")

	// 获取4小时数据 (大方向判断)
	klines4h, err := getKlinesFromAPI(symbol, "4h", 60)
	if err != nil {
		return nil, fmt.Errorf("获取4小时K线失败: %v", err)
	}
	data.Timeframe4h = calculateTimeframeData(klines4h, "4h")

	// 获取日线数据 (长期趋势)
	klines1d, err := getKlinesFromAPI(symbol, "1d", 90) // 获取90天日线数据
	if err != nil {
		return nil, fmt.Errorf("获取日线K线失败: %v", err)
	}
	data.Timeframe1d = calculateTimeframeData(klines1d, "1d")

	return data, nil
}

// getKlinesFromAPI 从Binance API获取K线数据
func getKlinesFromAPI(symbol, interval string, limit int) ([]Kline, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/klines?symbol=%s&interval=%s&limit=%d",
		symbol, interval, limit)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rawData [][]interface{}
	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, err
	}

	klines := make([]Kline, len(rawData))
	for i, item := range rawData {
		openTime := int64(item[0].(float64))
		open, _ := parseFloat(item[1])
		high, _ := parseFloat(item[2])
		low, _ := parseFloat(item[3])
		close, _ := parseFloat(item[4])
		volume, _ := parseFloat(item[5])
		closeTime := int64(item[6].(float64))

		klines[i] = Kline{
			OpenTime:  openTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			CloseTime: closeTime,
		}
	}

	return klines, nil
}

// calculateTimeframeData 计算单个时间框架数据
func calculateTimeframeData(klines []Kline, timeframe string) *TimeframeData {
	if len(klines) == 0 {
		return &TimeframeData{Timeframe: timeframe}
	}

	currentPrice := klines[len(klines)-1].Close

	// 提取价格序列
	priceSeries := make([]float64, len(klines))
	for i, k := range klines {
		priceSeries[i] = k.Close
	}

	// 计算技术指标
	ema20 := calculateEMAFromSeries(priceSeries, 20)
	ema50 := calculateEMAFromSeries(priceSeries, 50)
	macd := calculateMACDFromSeries(priceSeries)
	rsi7 := calculateRSIFromSeries(priceSeries, 7)
	rsi14 := calculateRSIFromSeries(priceSeries, 14)
	atr14 := calculateATRFromKlines(klines, 14)

	volume := 0.0
	if len(klines) > 0 {
		volume = klines[len(klines)-1].Volume
	}

	// 判断趋势方向
	trendDirection := determineTrendDirection(currentPrice, ema20, ema50, macd)

	// 计算信号强度
	signalStrength := calculateTimeframeSignalStrength(currentPrice, ema20, ema50, macd, rsi7)

	return &TimeframeData{
		Timeframe:      timeframe,
		CurrentPrice:   currentPrice,
		EMA20:          ema20,
		EMA50:          ema50,
		MACD:           macd,
		RSI7:           rsi7,
		RSI14:          rsi14,
		ATR14:          atr14,
		Volume:         volume,
		PriceSeries:    priceSeries,
		TrendDirection: trendDirection,
		SignalStrength: signalStrength,
	}
}

// ==================== 技术指标计算（从价格序列） ====================

// calculateEMAFromSeries 计算EMA (基于价格序列)
func calculateEMAFromSeries(prices []float64, period int) float64 {
	if len(prices) < period {
		return 0
	}

	// 计算SMA作为初始EMA
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += prices[i]
	}
	ema := sum / float64(period)

	// 计算EMA
	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(prices); i++ {
		ema = (prices[i]-ema)*multiplier + ema
	}

	return ema
}

// calculateMACDFromSeries 从价格序列计算MACD
func calculateMACDFromSeries(prices []float64) float64 {
	if len(prices) < 26 {
		return 0
	}

	ema12 := calculateEMAFromSeries(prices, 12)
	ema26 := calculateEMAFromSeries(prices, 26)

	return ema12 - ema26
}

// calculateRSIFromSeries 从价格序列计算RSI
func calculateRSIFromSeries(prices []float64, period int) float64 {
	if len(prices) <= period {
		return 0
	}

	gains := 0.0
	losses := 0.0

	for i := 1; i <= period; i++ {
		change := prices[i] - prices[i-1]
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// calculateATRFromKlines 从K线计算ATR
func calculateATRFromKlines(klines []Kline, period int) float64 {
	if len(klines) <= period {
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

// ==================== 趋势判断 ====================

// determineTrendDirection 判断趋势方向
func determineTrendDirection(price, ema20, ema50, macd float64) string {
	bullishSignals := 0
	bearishSignals := 0

	if price > ema20 && ema20 > 0 {
		bullishSignals++
	} else if price < ema20 && ema20 > 0 {
		bearishSignals++
	}

	if ema20 > ema50 && ema50 > 0 {
		bullishSignals++
	} else if ema20 < ema50 && ema50 > 0 {
		bearishSignals++
	}

	if macd > 0.001 {
		bullishSignals++
	} else if macd < -0.001 {
		bearishSignals++
	}

	if bullishSignals >= 2 {
		return "bullish"
	} else if bearishSignals >= 2 {
		return "bearish"
	}
	return "neutral"
}

// calculateTimeframeSignalStrength 计算时间框架信号强度
func calculateTimeframeSignalStrength(price, ema20, ema50, macd, rsi7 float64) int {
	strength := 50

	// 价格与EMA关系
	if price > ema20 && ema20 > ema50 {
		strength += 20
	} else if price < ema20 && ema20 < ema50 {
		strength -= 20
	}

	// MACD信号
	if macd > 0.001 {
		strength += 15
	} else if macd < -0.001 {
		strength -= 15
	}

	// RSI信号
	if rsi7 < 30 {
		strength += 10
	} else if rsi7 > 70 {
		strength -= 10
	}

	if strength < 0 {
		return 0
	}
	if strength > 100 {
		return 100
	}
	return strength
}

// calculatePriceChange 计算价格变化百分比
func calculatePriceChange(priceSeries []float64) float64 {
	if len(priceSeries) < 2 {
		return 0
	}
	current := priceSeries[len(priceSeries)-1]
	previous := priceSeries[0]
	if previous > 0 {
		return ((current - previous) / previous) * 100
	}
	return 0
}

// calculateSimpleATR 简化版ATR计算
func calculateSimpleATR(prices []float64, period int) float64 {
	if len(prices) < period {
		return 0
	}

	sum := 0.0
	for i := 1; i <= period; i++ {
		tr := math.Abs(prices[i] - prices[i-1])
		sum += tr
	}

	return sum / float64(period)
}

// calculateLongerTermDataFromKlines 计算长期数据（从K线）
func calculateLongerTermDataFromKlines(klines []Kline) *LongerTermData {
	data := &LongerTermData{
		MACDValues:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
	}

	if len(klines) == 0 {
		return data
	}

	// 提取价格序列
	priceSeries := make([]float64, len(klines))
	for i, k := range klines {
		priceSeries[i] = k.Close
	}

	// 计算EMA
	data.EMA20 = calculateEMAFromSeries(priceSeries, 20)
	data.EMA50 = calculateEMAFromSeries(priceSeries, 50)

	// 计算ATR
	data.ATR14 = calculateATRFromKlines(klines, 14)
	data.ATR3 = calculateATRFromKlines(klines, 3)

	// 成交量数据
	if len(klines) > 0 {
		data.CurrentVolume = klines[len(klines)-1].Volume
		// 计算平均成交量
		sum := 0.0
		for _, k := range klines {
			sum += k.Volume
		}
		data.AverageVolume = sum / float64(len(klines))
	}

	// 计算MACD和RSI序列
	start := len(priceSeries) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(priceSeries); i++ {
		if i >= 26 {
			macd := calculateMACDFromSeries(priceSeries[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}
		if i >= 14 {
			rsi14 := calculateRSIFromSeries(priceSeries[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	return data
}
