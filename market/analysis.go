package market

import (
	"fmt"
	"math"
	"strings"
)

// ==================== 斐波那契计算函数 ====================

// calculateFibonacciLevels 计算斐波那契回撤水平
func calculateFibonacciLevels(high, low float64) *FibLevels {
	diff := high - low
	return &FibLevels{
		Level236: high - (diff * 0.236),
		Level382: high - (diff * 0.382),
		Level500: high - (diff * 0.5),
		Level618: high - (diff * 0.618),
		Level705: high - (diff * 0.705),
		Level786: high - (diff * 0.786),
		High:     high,
		Low:      low,
		Trend:    "bullish", // 默认，实际使用时需要根据趋势判断
	}
}

// detectMarketStructure 检测市场结构
func detectMarketStructure(priceSeries []float64) *MarketStructure {
	if len(priceSeries) < 10 {
		return nil
	}

	structure := &MarketStructure{
		SwingHighs: make([]float64, 0),
		SwingLows:  make([]float64, 0),
	}

	// 简单的波段检测算法
	for i := 2; i < len(priceSeries)-2; i++ {
		// 检测波段高点
		if priceSeries[i] > priceSeries[i-1] && priceSeries[i] > priceSeries[i-2] &&
			priceSeries[i] > priceSeries[i+1] && priceSeries[i] > priceSeries[i+2] {
			structure.SwingHighs = append(structure.SwingHighs, priceSeries[i])
		}
		// 检测波段低点
		if priceSeries[i] < priceSeries[i-1] && priceSeries[i] < priceSeries[i-2] &&
			priceSeries[i] < priceSeries[i+1] && priceSeries[i] < priceSeries[i+2] {
			structure.SwingLows = append(structure.SwingLows, priceSeries[i])
		}
	}

	// 确定当前偏向
	if len(structure.SwingHighs) > 1 && len(structure.SwingLows) > 1 {
		latestHigh := structure.SwingHighs[len(structure.SwingHighs)-1]
		prevHigh := structure.SwingHighs[len(structure.SwingHighs)-2]
		latestLow := structure.SwingLows[len(structure.SwingLows)-1]
		prevLow := structure.SwingLows[len(structure.SwingLows)-2]

		if latestHigh > prevHigh && latestLow > prevLow {
			structure.CurrentBias = "bullish"
		} else if latestHigh < prevHigh && latestLow < prevLow {
			structure.CurrentBias = "bearish"
		} else {
			structure.CurrentBias = "neutral"
		}
	}

	return structure
}

// calculateCurrentFibLevels 计算当前斐波那契水平
func calculateCurrentFibLevels(structure *MarketStructure) *FibLevels {
	if structure == nil || len(structure.SwingHighs) < 2 || len(structure.SwingLows) < 2 {
		return nil
	}

	// 使用最近的波段高点和低点
	recentHigh := structure.SwingHighs[len(structure.SwingHighs)-1]
	recentLow := structure.SwingLows[len(structure.SwingLows)-1]

	// 确保高点高于低点
	if recentHigh <= recentLow {
		return nil
	}

	fibLevels := calculateFibonacciLevels(recentHigh, recentLow)
	fibLevels.Trend = structure.CurrentBias

	return fibLevels
}

// ==================== 震荡市检测相关函数 ====================

// DetectMarketCondition 检测市场状态
func DetectMarketCondition(data *Data) *MarketCondition {
	if data == nil {
		return &MarketCondition{Condition: "unknown", Confidence: 0}
	}

	condition := &MarketCondition{}

	// 使用现有数据计算市场状态
	atrRatio := calculateATRRatio(data)
	emaSlope := calculateEMASlope(data)
	priceChannel := calculatePriceChannel(data)
	rsiPosition := analyzeRSIPosition(data)
	timeframeConsistency := checkTimeframeConsistency(data)

	trendingScore, rangingScore := calculateMarketScores(
		atrRatio, emaSlope, priceChannel, rsiPosition, timeframeConsistency)

	if trendingScore > 70 {
		condition.Condition = "trending"
		condition.Confidence = trendingScore
	} else if rangingScore > 60 {
		condition.Condition = "ranging"
		condition.Confidence = rangingScore
	} else {
		condition.Condition = "volatile"
		condition.Confidence = 50
	}

	condition.ATRRatio = atrRatio
	condition.EMASlope = emaSlope
	condition.PriceChannel = priceChannel

	return condition
}

// calculateATRRatio 基于现有ATR数据计算波动率
func calculateATRRatio(data *Data) float64 {
	if data.LongerTermContext == nil || data.CurrentPrice == 0 {
		return 0
	}
	return (data.LongerTermContext.ATR14 / data.CurrentPrice) * 100
}

// calculateEMASlope 基于现有EMA数据计算斜率
func calculateEMASlope(data *Data) float64 {
	// 方法1：使用多时间框架EMA值估算斜率
	if data.MultiTimeframe != nil {
		var emaValues []float64
		if data.MultiTimeframe.Timeframe15m != nil {
			emaValues = append(emaValues, data.MultiTimeframe.Timeframe15m.EMA20)
		}
		if data.MultiTimeframe.Timeframe1h != nil {
			emaValues = append(emaValues, data.MultiTimeframe.Timeframe1h.EMA20)
		}
		if data.MultiTimeframe.Timeframe4h != nil {
			emaValues = append(emaValues, data.MultiTimeframe.Timeframe4h.EMA20)
		}
		if data.MultiTimeframe.Timeframe1d != nil {
			emaValues = append(emaValues, data.MultiTimeframe.Timeframe1d.EMA20)
		}

		if len(emaValues) >= 2 {
			// 计算EMA变化的百分比斜率
			slope := (emaValues[len(emaValues)-1] - emaValues[0]) / emaValues[0] * 100
			return slope
		}
	}

	// 方法2：使用当前EMA和历史EMA（如果有）
	if data.LongerTermContext != nil && data.LongerTermContext.EMA20 != 0 {
		slope := (data.CurrentEMA20 - data.LongerTermContext.EMA20) / data.LongerTermContext.EMA20 * 100
		return slope
	}

	return 0
}

// calculatePriceChannel 计算价格通道宽度
func calculatePriceChannel(data *Data) float64 {
	// 使用多时间框架的最高最低EMA估算通道
	if data.MultiTimeframe == nil {
		return 0
	}

	var emas []float64
	if data.MultiTimeframe.Timeframe15m != nil {
		emas = append(emas, data.MultiTimeframe.Timeframe15m.EMA20)
	}
	if data.MultiTimeframe.Timeframe1h != nil {
		emas = append(emas, data.MultiTimeframe.Timeframe1h.EMA20)
	}
	if data.MultiTimeframe.Timeframe4h != nil {
		emas = append(emas, data.MultiTimeframe.Timeframe4h.EMA20)
	}
	if data.MultiTimeframe.Timeframe1d != nil {
		emas = append(emas, data.MultiTimeframe.Timeframe1d.EMA20)
	}

	if len(emas) < 2 {
		return 0
	}

	// 找到EMA的最大最小值
	minEMA, maxEMA := emas[0], emas[0]
	for _, ema := range emas {
		if ema < minEMA {
			minEMA = ema
		}
		if ema > maxEMA {
			maxEMA = ema
		}
	}

	channelWidth := (maxEMA - minEMA) / data.CurrentPrice * 100
	return channelWidth
}

// analyzeRSIPosition 分析RSI位置
func analyzeRSIPosition(data *Data) float64 {
	// 使用现有RSI数据判断是否在震荡区间
	rsiValue := data.CurrentRSI7

	// 判断RSI是否在震荡区间 (30-70)
	if rsiValue >= 30 && rsiValue <= 70 {
		return 80 // 高概率震荡
	} else if rsiValue >= 40 && rsiValue <= 60 {
		return 95 // 极高概率震荡
	} else {
		return 30 // 低概率震荡
	}
}

// checkTimeframeConsistency 检查多时间框架一致性
func checkTimeframeConsistency(data *Data) float64 {
	if data.MultiTimeframe == nil {
		return 0
	}

	timeframes := []*TimeframeData{
		data.MultiTimeframe.Timeframe15m,
		data.MultiTimeframe.Timeframe1h,
		data.MultiTimeframe.Timeframe4h,
		data.MultiTimeframe.Timeframe1d,
	}

	bullishCount, bearishCount := 0, 0
	validCount := 0

	for _, tf := range timeframes {
		if tf != nil {
			validCount++
			if tf.TrendDirection == "bullish" {
				bullishCount++
			} else if tf.TrendDirection == "bearish" {
				bearishCount++
			}
		}
	}

	if validCount == 0 {
		return 0
	}

	// 计算一致性得分
	consistency := math.Max(float64(bullishCount), float64(bearishCount)) / float64(validCount) * 100
	return consistency
}

// calculateMarketScores 计算市场状态得分
func calculateMarketScores(atrRatio, emaSlope, priceChannel, rsiPosition, timeframeConsistency float64) (int, int) {
	trendingScore, rangingScore := 0, 0

	// 趋势市特征
	if math.Abs(emaSlope) > 0.1 { // EMA有明显斜率
		trendingScore += 25
	}
	if atrRatio > 0.3 { // 波动率适中偏高
		trendingScore += 20
	}
	if timeframeConsistency > 70 { // 多时间框架一致
		trendingScore += 30
	}
	if rsiPosition < 50 { // RSI不在中间区域
		trendingScore += 25
	}

	// 震荡市特征
	if math.Abs(emaSlope) < 0.05 { // EMA走平
		rangingScore += 30
	}
	if priceChannel < 2.0 { // 价格通道狭窄
		rangingScore += 25
	}
	if rsiPosition > 70 { // RSI常在中间区域
		rangingScore += 25
	}
	if timeframeConsistency < 50 { // 多时间框架不一致
		rangingScore += 20
	}

	return trendingScore, rangingScore
}

// IsRangingMarket 判断是否为震荡市
func IsRangingMarket(data *Data) bool {
	condition := DetectMarketCondition(data)
	return condition.Condition == "ranging" && condition.Confidence > 60
}

// ShouldAvoidTrading 是否应避免交易
func ShouldAvoidTrading(data *Data) (bool, string) {
	if data == nil {
		return true, "数据无效"
	}

	// 检查震荡市
	marketCondition := DetectMarketCondition(data)
	if marketCondition.Condition == "ranging" && marketCondition.Confidence > 60 {
		return true, fmt.Sprintf("高置信度震荡市(%d%%)，建议观望", marketCondition.Confidence)
	}

	// 检查其他不适合交易的条件
	if valid, reason := ValidateForTrading(data); !valid {
		return true, reason
	}

	return false, "适合交易"
}

// ValidateForTrading 验证是否适合交易
func ValidateForTrading(data *Data) (bool, string) {
	if data == nil {
		return false, "数据无效"
	}

	// 检查持仓量
	if data.OpenInterest != nil && data.OpenInterest.Latest > 0 {
		oiValue := data.OpenInterest.Latest * data.CurrentPrice
		oiValueInMillions := oiValue / 1_000_000
		if oiValueInMillions < 15 {
			return false, fmt.Sprintf("持仓价值过低(%.2fM USD < 15M)", oiValueInMillions)
		}
	}

	// 检查信号强度
	if !IsStrongSignal(data) {
		signalStrength := GetSignalStrength(data)
		trendSummary := GetTrendSummary(data)
		return false, fmt.Sprintf("信号强度不足(强度:%d/70, 趋势:%s)", signalStrength, trendSummary)
	}

	// 检查风险等级
	riskLevel := GetRiskLevel(data)
	if riskLevel == "🔴 高风险" {
		return false, "风险等级过高"
	}

	// 检查震荡市
	marketCondition := DetectMarketCondition(data)
	if marketCondition.Condition == "ranging" && marketCondition.Confidence > 60 {
		return false, fmt.Sprintf("震荡市(置信度%d%%)，避免开仓", marketCondition.Confidence)
	}

	return true, "适合交易"
}

// ==================== 趋势和信号分析 ====================

// GetTrendSummary 获取趋势摘要
func GetTrendSummary(data *Data) string {
	if data == nil || data.MultiTimeframe == nil {
		return "数据不足"
	}

	var bullishCount, bearishCount, neutralCount int

	// 统计各时间框架趋势
	timeframes := []*TimeframeData{
		data.MultiTimeframe.Timeframe15m,
		data.MultiTimeframe.Timeframe1h,
		data.MultiTimeframe.Timeframe4h,
		data.MultiTimeframe.Timeframe1d,
	}

	for _, tf := range timeframes {
		if tf != nil {
			switch tf.TrendDirection {
			case "bullish":
				bullishCount++
			case "bearish":
				bearishCount++
			case "neutral":
				neutralCount++
			}
		}
	}

	// 判断总体趋势
	if bullishCount >= 2 {
		return "📈 多头趋势"
	} else if bearishCount >= 2 {
		return "📉 空头趋势"
	} else if neutralCount >= 2 {
		return "➡️ 震荡整理"
	} else {
		return "🔀 趋势不明"
	}
}

// GetSignalStrength 获取综合信号强度
func GetSignalStrength(data *Data) int {
	fmt.Printf("📊 [GetSignalStrength] 开始计算综合信号强度\n")

	// 数据有效性检查
	if data == nil {
		fmt.Printf("❌ [GetSignalStrength] data为nil，返回0\n")
		return 0
	}
	if data.MultiTimeframe == nil {
		fmt.Printf("❌ [GetSignalStrength] MultiTimeframe为nil，返回0\n")
		return 0
	}
	fmt.Printf("✅ [GetSignalStrength] 数据有效性检查通过\n")

	var totalStrength int
	var count int

	// 计算各时间框架信号强度的平均值
	timeframes := []*TimeframeData{
		data.MultiTimeframe.Timeframe15m,
		data.MultiTimeframe.Timeframe1h,
		data.MultiTimeframe.Timeframe4h,
		data.MultiTimeframe.Timeframe1d,
	}

	timeframeNames := []string{"15m", "1h", "4h", "1d"}

	fmt.Printf("🔍 [GetSignalStrength] 遍历4个时间框架收集信号强度...\n")
	for i, tf := range timeframes {
		tfName := timeframeNames[i]
		if tf != nil {
			fmt.Printf("   ├─ %s: SignalStrength=%d, TrendDirection=%s\n",
				tfName, tf.SignalStrength, tf.TrendDirection)
			totalStrength += tf.SignalStrength
			count++
		} else {
			fmt.Printf("   ├─ %s: nil (跳过)\n", tfName)
		}
	}

	fmt.Printf("📈 [GetSignalStrength] 统计结果:\n")
	fmt.Printf("   ├─ 有效时间框架数: %d/4\n", count)
	fmt.Printf("   ├─ 总信号强度: %d\n", totalStrength)

	if count > 0 {
		avgStrength := totalStrength / count
		fmt.Printf("   ├─ 平均信号强度: %d / %d = %d\n", totalStrength, count, avgStrength)
		fmt.Printf("✅ [GetSignalStrength] 计算完成，返回综合信号强度: %d\n", avgStrength)
		return avgStrength
	}

	fmt.Printf("⚠️  [GetSignalStrength] 无有效时间框架数据，返回0\n")
	return 0
}

// IsStrongSignal 判断是否为强信号
func IsStrongSignal(data *Data) bool {
	signalStrength := GetSignalStrength(data)
	trendSummary := GetTrendSummary(data)

	// 强信号标准：信号强度>70且趋势明确
	return signalStrength > 70 && (trendSummary == "📈 多头趋势" || trendSummary == "📉 空头趋势")
}

// GetRiskLevel 获取风险等级
func GetRiskLevel(data *Data) string {
	if data == nil {
		return "未知"
	}

	rsi := data.CurrentRSI7
	macd := data.CurrentMACD

	// 基于RSI和MACD判断风险
	if rsi > 80 || rsi < 20 {
		return "🔴 高风险"
	} else if (rsi > 70 && macd < 0) || (rsi < 30 && macd > 0) {
		return "🟡 中风险"
	} else {
		return "🟢 低风险"
	}
}

// GetTradingRecommendation 获取交易建议
func GetTradingRecommendation(data *Data) string {
	if data == nil {
		return "观望"
	}

	trend := GetTrendSummary(data)
	signalStrength := GetSignalStrength(data)
	riskLevel := GetRiskLevel(data)

	if signalStrength < 60 {
		return "观望"
	}

	switch trend {
	case "📈 多头趋势":
		if riskLevel == "🟢 低风险" {
			return "考虑做多"
		} else if riskLevel == "🟡 中风险" {
			return "谨慎做多"
		} else {
			return "观望"
		}
	case "📉 空头趋势":
		if riskLevel == "🟢 低风险" {
			return "考虑做空"
		} else if riskLevel == "🟡 中风险" {
			return "谨慎做空"
		} else {
			return "观望"
		}
	default:
		return "观望"
	}
}

// GetPriceTargets 获取价格目标
func GetPriceTargets(data *Data) (float64, float64) {
	if data == nil {
		return 0, 0
	}

	currentPrice := data.CurrentPrice
	atr := data.LongerTermContext.ATR14

	// 基于ATR计算止损和止盈
	stopLoss := currentPrice - (atr * 2)   // 2倍ATR止损
	takeProfit := currentPrice + (atr * 6) // 6倍ATR止盈（风险回报比1:3）

	return stopLoss, takeProfit
}

// GetMarketConditionSummary 获取市场状态摘要
func GetMarketConditionSummary(data *Data) string {
	if data == nil {
		return "数据不足"
	}

	condition := DetectMarketCondition(data)

	switch condition.Condition {
	case "trending":
		return fmt.Sprintf("📈 趋势市(置信度%d%%)", condition.Confidence)
	case "ranging":
		return fmt.Sprintf("🔄 震荡市(置信度%d%%)", condition.Confidence)
	case "volatile":
		return fmt.Sprintf("🌊 波动市(置信度%d%%)", condition.Confidence)
	default:
		return "🔍 状态不明"
	}
}

// FormatMarketData 格式化市场数据输出（完整版本）
func FormatMarketData(data *Data) string {
	if data == nil {
		return "无市场数据"
	}

	var sb strings.Builder

	// 基础价格信息
	sb.WriteString(fmt.Sprintf("💰 当前价格: %.4f | 1h: %+.2f%% | 4h: %+.2f%% | 1d: %+.2f%%\n",
		data.CurrentPrice, data.PriceChange1h, data.PriceChange4h, data.PriceChange1d))

	// 技术指标
	sb.WriteString(fmt.Sprintf("📊 EMA20: %.4f | MACD: %.4f | RSI7: %.1f\n",
		data.CurrentEMA20, data.CurrentMACD, data.CurrentRSI7))

	// 多时间框架分析
	if data.MultiTimeframe != nil {
		sb.WriteString("⏰ 多时间框架:\n")

		// 15分钟框架
		if tf15 := data.MultiTimeframe.Timeframe15m; tf15 != nil {
			sb.WriteString(fmt.Sprintf("   • 15m: %s(强度%d) | EMA20:%.4f | MACD:%.4f | RSI:%.1f\n",
				tf15.TrendDirection, tf15.SignalStrength, tf15.EMA20, tf15.MACD, tf15.RSI7))
		}

		// 1小时框架
		if tf1h := data.MultiTimeframe.Timeframe1h; tf1h != nil {
			sb.WriteString(fmt.Sprintf("   • 1h:  %s(强度%d) | EMA20:%.4f | MACD:%.4f | RSI:%.1f\n",
				tf1h.TrendDirection, tf1h.SignalStrength, tf1h.EMA20, tf1h.MACD, tf1h.RSI7))
		}

		// 4小时框架
		if tf4h := data.MultiTimeframe.Timeframe4h; tf4h != nil {
			sb.WriteString(fmt.Sprintf("   • 4h:  %s(强度%d) | EMA20:%.4f | MACD:%.4f | RSI:%.1f\n",
				tf4h.TrendDirection, tf4h.SignalStrength, tf4h.EMA20, tf4h.MACD, tf4h.RSI7))
		}

		// 日线框架
		if tf1d := data.MultiTimeframe.Timeframe1d; tf1d != nil {
			sb.WriteString(fmt.Sprintf("   • 1d:  %s(强度%d) | EMA20:%.4f | MACD:%.4f | RSI:%.1f\n",
				tf1d.TrendDirection, tf1d.SignalStrength, tf1d.EMA20, tf1d.MACD, tf1d.RSI7))
		}
	}

	// 资金数据
	if data.OpenInterest != nil {
		sb.WriteString(fmt.Sprintf("📈 持仓量: %.0f | 平均: %.0f\n",
			data.OpenInterest.Latest, data.OpenInterest.Average))
	}

	sb.WriteString(fmt.Sprintf("💸 资金费率: %.4f%%\n", data.FundingRate*100))

	// 长期数据
	if data.LongerTermContext != nil {
		sb.WriteString("📅 长期数据:\n")
		sb.WriteString(fmt.Sprintf("   • EMA20: %.4f | EMA50: %.4f\n",
			data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))
		sb.WriteString(fmt.Sprintf("   • ATR3: %.4f | ATR14: %.4f\n",
			data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))
		sb.WriteString(fmt.Sprintf("   • 成交量: %.0f | 平均: %.0f\n",
			data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))

		// MACD序列
		if len(data.LongerTermContext.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("   • MACD序列: %.4f → %.4f\n",
				data.LongerTermContext.MACDValues[0],
				data.LongerTermContext.MACDValues[len(data.LongerTermContext.MACDValues)-1]))
		}

		// RSI序列
		if len(data.LongerTermContext.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("   • RSI序列: %.1f → %.1f\n",
				data.LongerTermContext.RSI14Values[0],
				data.LongerTermContext.RSI14Values[len(data.LongerTermContext.RSI14Values)-1]))
		}
	}

	// 市场状态显示
	marketCondition := DetectMarketCondition(data)
	sb.WriteString(fmt.Sprintf("🌊 市场状态: %s (置信度: %d%%)\n",
		marketCondition.Condition, marketCondition.Confidence))
	sb.WriteString(fmt.Sprintf("   • EMA斜率: %.4f%% | 价格通道: %.2f%% | ATR比率: %.2f%%\n",
		marketCondition.EMASlope, marketCondition.PriceChannel, marketCondition.ATRRatio))

	// 市场结构和斐波那契信息
	if data.MarketStructure != nil {
		sb.WriteString("🏗️ 市场结构:\n")
		sb.WriteString(fmt.Sprintf("   • 偏向: %s | 波段高点: %d | 波段低点: %d\n",
			data.MarketStructure.CurrentBias,
			len(data.MarketStructure.SwingHighs),
			len(data.MarketStructure.SwingLows)))

		if len(data.MarketStructure.SwingHighs) > 0 && len(data.MarketStructure.SwingLows) > 0 {
			sb.WriteString(fmt.Sprintf("   • 最近波段: %.4f → %.4f\n",
				data.MarketStructure.SwingHighs[len(data.MarketStructure.SwingHighs)-1],
				data.MarketStructure.SwingLows[len(data.MarketStructure.SwingLows)-1]))
		}
	}

	if data.FibLevels != nil {
		sb.WriteString("📐 斐波那契水平:\n")
		sb.WriteString(fmt.Sprintf("   • 0.5中线: %.4f | 0.618: %.4f | 0.705: %.4f\n",
			data.FibLevels.Level500, data.FibLevels.Level618, data.FibLevels.Level705))
		sb.WriteString(fmt.Sprintf("   • OTE区间: %.4f - %.4f\n",
			data.FibLevels.Level618, data.FibLevels.Level705))

		// 显示当前价格相对于斐波那契水平的位置
		currentPrice := data.CurrentPrice
		if currentPrice >= data.FibLevels.Level705 && currentPrice <= data.FibLevels.Level618 {
			sb.WriteString("   🎯 **当前价格在OTE黄金区间内**\n")
		} else if currentPrice > data.FibLevels.Level500 {
			sb.WriteString("   🔴 当前价格在溢价区\n")
		} else {
			sb.WriteString("   🟢 当前价格在折扣区\n")
		}
	}

	// 震荡市警告
	if marketCondition.Condition == "ranging" && marketCondition.Confidence > 60 {
		sb.WriteString("🚨 **震荡市警告**: 避免开仓，耐心等待趋势突破！\n")
	}

	return sb.String()
}
