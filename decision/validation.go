package decision

import (
	"fmt"
	"nofx/market"
	"strings"
)

// ==================== 市场状态过滤函数 ====================

// shouldSkipSymbol 判断是否应该跳过某个币种
func shouldSkipSymbol(data *market.Data, symbol string) string {
	fmt.Printf("📊 [shouldSkipSymbol] 开始检查币种: %s\n", symbol)

	// 数据有效性检查
	if data == nil {
		fmt.Printf("❌ [shouldSkipSymbol] %s - 数据为nil，跳过\n", symbol)
		return "数据无效"
	}
	fmt.Printf("✅ [shouldSkipSymbol] %s - 数据有效性检查通过\n", symbol)

	// 1. 流动性过滤：持仓价值低于15M USD
	fmt.Printf("🔍 [shouldSkipSymbol] %s - 步骤1: 检查流动性过滤...\n", symbol)
	if data.OpenInterest != nil && data.CurrentPrice > 0 {
		oiValue := data.OpenInterest.Latest * data.CurrentPrice
		oiValueInMillions := oiValue / 1_000_000
		fmt.Printf("   ├─ OI.Latest=%.2f, CurrentPrice=%.2f\n", data.OpenInterest.Latest, data.CurrentPrice)
		fmt.Printf("   ├─ OI值计算: %.2f * %.2f = %.2f USDT\n", data.OpenInterest.Latest, data.CurrentPrice, oiValue)
		fmt.Printf("   ├─ OI值(M): %.2fM USD\n", oiValueInMillions)

		if oiValueInMillions < 15 {
			fmt.Printf("❌ [shouldSkipSymbol] %s - 流动性检查失败: %.2fM USD < 15M，跳过\n", symbol, oiValueInMillions)
			return fmt.Sprintf("持仓价值过低(%.2fM USD < 15M)", oiValueInMillions)
		}
		fmt.Printf("✅ [shouldSkipSymbol] %s - 流动性检查通过: %.2fM USD >= 15M\n", symbol, oiValueInMillions)
	} else {
		// 记录异常情况
		if data.OpenInterest == nil {
			fmt.Printf("⚠️  [shouldSkipSymbol] %s - 流动性检查异常: OpenInterest为nil\n", symbol)
		} else if data.CurrentPrice <= 0 {
			fmt.Printf("⚠️  [shouldSkipSymbol] %s - 流动性检查异常: CurrentPrice=%.2f (<=0)\n", symbol, data.CurrentPrice)
		}
	}

	// 2. 市场状态过滤：高置信度震荡市
	fmt.Printf("🔍 [shouldSkipSymbol] %s - 步骤2: 检查市场状态过滤...\n", symbol)
	isRanging := market.IsRangingMarket(data)
	fmt.Printf("   ├─ IsRangingMarket结果: %v\n", isRanging)

	if isRanging {
		condition := market.DetectMarketCondition(data)
		fmt.Printf("   ├─ 市场状态: %s, 置信度: %d%%\n", condition.Condition, condition.Confidence)
		fmt.Printf("❌ [shouldSkipSymbol] %s - 市场状态检查失败: 高置信度震荡市(%d%%)，跳过\n", symbol, condition.Confidence)
		return fmt.Sprintf("高置信度震荡市(%d%%)", condition.Confidence)
	}
	fmt.Printf("✅ [shouldSkipSymbol] %s - 市场状态检查通过: 非震荡市\n", symbol)

	// 3. 交易适合性检查
	fmt.Printf("🔍 [shouldSkipSymbol] %s - 步骤3: 检查交易适合性...\n", symbol)
	shouldAvoid, reason := market.ShouldAvoidTrading(data)
	fmt.Printf("   ├─ ShouldAvoidTrading结果: shouldAvoid=%v\n", shouldAvoid)
	if reason != "" {
		fmt.Printf("   ├─ 原因: %s\n", reason)
	}

	if shouldAvoid {
		fmt.Printf("❌ [shouldSkipSymbol] %s - 交易适合性检查失败: %s，跳过\n", symbol, reason)
		return reason
	}
	fmt.Printf("✅ [shouldSkipSymbol] %s - 交易适合性检查通过\n", symbol)

	fmt.Printf("🎉 [shouldSkipSymbol] %s - 所有检查通过，可以交易\n", symbol)
	return ""
}

// ==================== 决策验证函数 ====================

// ValidateDecisionWithMarketData 使用市场数据验证决策
func ValidateDecisionWithMarketData(decision *Decision, marketData *market.Data, account *AccountInfo) (bool, string) {
	if decision == nil {
		return false, "决策为空"
	}

	// 检查市场数据
	if marketData == nil {
		return false, "市场数据不可用"
	}

	// 检查震荡市（对开仓操作）
	if decision.Action == "open_long" || decision.Action == "open_short" {
		if shouldAvoid, reason := market.ShouldAvoidTrading(marketData); shouldAvoid {
			return false, fmt.Sprintf("市场状态不适合开仓: %s", reason)
		}
	}

	// 检查持仓价值
	if marketData.OpenInterest != nil && marketData.CurrentPrice > 0 {
		oiValue := marketData.OpenInterest.Latest * marketData.CurrentPrice
		oiValueInMillions := oiValue / 1_000_000
		if oiValueInMillions < 15 {
			return false, fmt.Sprintf("持仓价值过低(%.2fM USD < 15M)", oiValueInMillions)
		}
	}

	// 检查仓位大小
	if decision.PositionSizeUSD > 0 {
		// 确保单笔风险不超过账户净值的2%
		maxRisk := account.TotalEquity * 0.02
		if decision.RiskUSD > maxRisk {
			return false, fmt.Sprintf("风险过大(%.2f > 最大%.2f)", decision.RiskUSD, maxRisk)
		}
	}

	// 检查保证金使用率
	if account.MarginUsedPct > 50 {
		return false, fmt.Sprintf("保证金使用率过高(%.1f%% > 50%%)", account.MarginUsedPct)
	}

	return true, "决策有效"
}

// FilterValidDecisions 过滤有效的决策
func FilterValidDecisions(decisions []Decision, marketDataMap map[string]*market.Data, account *AccountInfo) []Decision {
	validDecisions := make([]Decision, 0)

	for _, decision := range decisions {
		marketData, exists := marketDataMap[decision.Symbol]
		if !exists {
			continue
		}

		if valid, _ := ValidateDecisionWithMarketData(&decision, marketData, account); valid {
			validDecisions = append(validDecisions, decision)
		}
	}

	return validDecisions
}

// ==================== 决策摘要函数 ====================

// GetDecisionSummary 获取决策摘要
func GetDecisionSummary(decision *FullDecision) string {
	if decision == nil || len(decision.Decisions) == 0 {
		return "🤔 无交易决策"
	}

	var sb strings.Builder
	sb.WriteString("🎯 交易决策摘要:\n")

	for _, d := range decision.Decisions {
		actionEmoji := getActionEmoji(d.Action)
		sb.WriteString(fmt.Sprintf("%s %s: %s", actionEmoji, d.Symbol, d.Action))

		if d.PositionSizeUSD > 0 {
			sb.WriteString(fmt.Sprintf(" | 仓位: $%.2f", d.PositionSizeUSD))
		}
		if d.Leverage > 0 {
			sb.WriteString(fmt.Sprintf(" | 杠杆: %dx", d.Leverage))
		}
		if d.Confidence > 0 {
			sb.WriteString(fmt.Sprintf(" | 信心: %d%%", d.Confidence))
		}
		sb.WriteString("\n")

		if d.Reasoning != "" {
			sb.WriteString(fmt.Sprintf("   📝 理由: %s\n", d.Reasoning))
		}
	}

	return sb.String()
}

// getActionEmoji 获取动作对应的emoji
func getActionEmoji(action string) string {
	switch action {
	case "open_long":
		return "🟢"
	case "open_short":
		return "🔴"
	case "close_long", "close_short":
		return "🟡"
	case "hold":
		return "🟣"
	case "wait":
		return "🔵"
	default:
		return "⚪"
	}
}

// ==================== 市场状态分析函数 ====================

// AnalyzeMarketConditions 分析整体市场状态
func AnalyzeMarketConditions(ctx *Context) string {
	var sb strings.Builder

	trendingCount, rangingCount, volatileCount := 0, 0, 0
	var rangingSymbols []string

	for symbol, data := range ctx.MarketDataMap {
		condition := market.DetectMarketCondition(data)
		switch condition.Condition {
		case "trending":
			trendingCount++
		case "ranging":
			rangingCount++
			rangingSymbols = append(rangingSymbols, symbol)
		case "volatile":
			volatileCount++
		}
	}

	total := len(ctx.MarketDataMap)
	if total == 0 {
		return "无市场数据"
	}

	sb.WriteString(fmt.Sprintf("🌊 市场状态分析 (%d个币种):\n", total))
	sb.WriteString(fmt.Sprintf("📈 趋势市: %d (%.1f%%)\n", trendingCount, float64(trendingCount)/float64(total)*100))
	sb.WriteString(fmt.Sprintf("🔄 震荡市: %d (%.1f%%)\n", rangingCount, float64(rangingCount)/float64(total)*100))
	sb.WriteString(fmt.Sprintf("🌊 波动市: %d (%.1f%%)\n", volatileCount, float64(volatileCount)/float64(total)*100))

	if rangingCount > total/2 {
		sb.WriteString("\n🚨 **市场警告**: 超过50%的币种处于震荡状态！\n")
		sb.WriteString("建议策略:\n")
		sb.WriteString("• 避免新开仓位\n")
		sb.WriteString("• 现有持仓考虑减仓\n")
		sb.WriteString("• 耐心等待趋势突破\n")
	}

	if len(rangingSymbols) > 0 {
		sb.WriteString(fmt.Sprintf("\n🔄 震荡币种: %s\n", strings.Join(rangingSymbols, ", ")))
	}

	return sb.String()
}

// ==================== 决策质量评估 ====================

// EvaluateDecisionQuality 评估决策质量
func EvaluateDecisionQuality(decision *Decision, marketData *market.Data) (int, string) {
	if decision == nil || marketData == nil {
		return 0, "无效决策"
	}

	score := 50 // 基础分
	var reasons []string

	// 1. 趋势一致性检查（20分）
	if marketData.MultiTimeframe != nil {
		trendSummary := market.GetTrendSummary(marketData)
		if decision.Action == "open_long" && trendSummary == "📈 多头趋势" {
			score += 20
			reasons = append(reasons, "✅ 顺势做多")
		} else if decision.Action == "open_short" && trendSummary == "📉 空头趋势" {
			score += 20
			reasons = append(reasons, "✅ 顺势做空")
		} else if decision.Action == "open_long" || decision.Action == "open_short" {
			score -= 10
			reasons = append(reasons, "⚠️ 趋势不明确")
		}
	}

	// 2. 信号强度检查（15分）
	signalStrength := market.GetSignalStrength(marketData)
	if signalStrength > 75 {
		score += 15
		reasons = append(reasons, "✅ 信号强度高")
	} else if signalStrength < 50 {
		score -= 10
		reasons = append(reasons, "⚠️ 信号强度弱")
	}

	// 3. 市场状态检查（15分）
	condition := market.DetectMarketCondition(marketData)
	if condition.Condition == "trending" {
		score += 15
		reasons = append(reasons, "✅ 趋势市")
	} else if condition.Condition == "ranging" {
		score -= 20
		reasons = append(reasons, "❌ 震荡市")
	}

	// 4. 风险回报比检查（如果是开仓）（20分）
	if decision.Action == "open_long" || decision.Action == "open_short" {
		if decision.Confidence >= 80 {
			score += 10
			reasons = append(reasons, "✅ 高信心度")
		} else if decision.Confidence < 70 {
			score -= 10
			reasons = append(reasons, "⚠️ 信心度不足")
		}
	}

	// 5. 流动性检查（10分）
	if marketData.OpenInterest != nil && marketData.CurrentPrice > 0 {
		oiValue := marketData.OpenInterest.Latest * marketData.CurrentPrice
		oiValueInMillions := oiValue / 1_000_000
		if oiValueInMillions >= 50 {
			score += 10
			reasons = append(reasons, "✅ 流动性充足")
		} else if oiValueInMillions < 15 {
			score -= 20
			reasons = append(reasons, "❌ 流动性不足")
		}
	}

	// 确保分数在0-100之间
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	reasonText := strings.Join(reasons, " | ")
	return score, reasonText
}

// ==================== 风险评估函数 ====================

// AssessPortfolioRisk 评估整体组合风险
func AssessPortfolioRisk(ctx *Context) string {
	var sb strings.Builder

	sb.WriteString("📊 组合风险评估:\n\n")

	// 1. 保证金使用率
	sb.WriteString(fmt.Sprintf("💰 保证金使用率: %.1f%%", ctx.Account.MarginUsedPct))
	if ctx.Account.MarginUsedPct > 80 {
		sb.WriteString(" ⚠️ 过高\n")
	} else if ctx.Account.MarginUsedPct > 60 {
		sb.WriteString(" 🟡 偏高\n")
	} else {
		sb.WriteString(" ✅ 正常\n")
	}

	// 2. 持仓数量
	sb.WriteString(fmt.Sprintf("📈 持仓数量: %d", ctx.Account.PositionCount))
	if ctx.Account.PositionCount > 5 {
		sb.WriteString(" ⚠️ 过多\n")
	} else if ctx.Account.PositionCount > 3 {
		sb.WriteString(" 🟡 偏多\n")
	} else {
		sb.WriteString(" ✅ 正常\n")
	}

	// 3. 总盈亏
	sb.WriteString(fmt.Sprintf("💵 总盈亏: %+.2f%%", ctx.Account.TotalPnLPct))
	if ctx.Account.TotalPnLPct < -5 {
		sb.WriteString(" ❌ 严重亏损\n")
	} else if ctx.Account.TotalPnLPct < 0 {
		sb.WriteString(" 🟡 亏损\n")
	} else if ctx.Account.TotalPnLPct > 10 {
		sb.WriteString(" 🎉 高收益\n")
	} else {
		sb.WriteString(" ✅ 盈利\n")
	}

	// 4. 持仓风险评估
	if len(ctx.Positions) > 0 {
		sb.WriteString("\n📋 持仓风险明细:\n")
		for i, pos := range ctx.Positions {
			riskLevel := "正常"
			if pos.UnrealizedPnLPct < -5 {
				riskLevel = "高风险"
			} else if pos.UnrealizedPnLPct < -2 {
				riskLevel = "中风险"
			}

			sb.WriteString(fmt.Sprintf("%d. %s %s: 盈亏%+.2f%% (%s)\n",
				i+1, pos.Symbol, strings.ToUpper(pos.Side),
				pos.UnrealizedPnLPct, riskLevel))
		}
	}

	return sb.String()
}

// ==================== 交易建议生成 ====================

// GenerateTradingAdvice 生成交易建议
func GenerateTradingAdvice(ctx *Context) string {
	var sb strings.Builder

	sb.WriteString("💡 交易建议:\n\n")

	// 1. 基于市场状态的建议
	trendingCount, rangingCount := 0, 0
	for _, data := range ctx.MarketDataMap {
		condition := market.DetectMarketCondition(data)
		if condition.Condition == "trending" {
			trendingCount++
		} else if condition.Condition == "ranging" {
			rangingCount++
		}
	}

	if rangingCount > len(ctx.MarketDataMap)/2 {
		sb.WriteString("🔄 **震荡市主导**:\n")
		sb.WriteString("  • 建议观望，避免新开仓\n")
		sb.WriteString("  • 现有持仓考虑减仓\n")
		sb.WriteString("  • 等待趋势突破信号\n\n")
	} else if trendingCount > len(ctx.MarketDataMap)/2 {
		sb.WriteString("📈 **趋势市主导**:\n")
		sb.WriteString("  • 可以寻找高质量开仓机会\n")
		sb.WriteString("  • 顺势而为，多空均可\n")
		sb.WriteString("  • 严格执行风险管理\n\n")
	}

	// 2. 基于账户状态的建议
	if ctx.Account.MarginUsedPct > 70 {
		sb.WriteString("⚠️ **保证金使用率高**:\n")
		sb.WriteString("  • 不建议开新仓\n")
		sb.WriteString("  • 考虑平掉部分持仓\n")
		sb.WriteString("  • 降低总体杠杆\n\n")
	}

	if ctx.Account.TotalPnLPct < -3 {
		sb.WriteString("📉 **账户亏损**:\n")
		sb.WriteString("  • 提高开仓标准，只做高信心度交易\n")
		sb.WriteString("  • 减少交易频率\n")
		sb.WriteString("  • 检查策略有效性\n\n")
	}

	// 3. 基于持仓的建议
	if len(ctx.Positions) > 0 {
		sb.WriteString("📋 **持仓管理**:\n")
		for _, pos := range ctx.Positions {
			if pos.UnrealizedPnLPct > 10 {
				sb.WriteString(fmt.Sprintf("  • %s: 考虑部分止盈（已盈利%.2f%%）\n",
					pos.Symbol, pos.UnrealizedPnLPct))
			} else if pos.UnrealizedPnLPct < -5 {
				sb.WriteString(fmt.Sprintf("  • %s: 考虑止损（已亏损%.2f%%）\n",
					pos.Symbol, pos.UnrealizedPnLPct))
			}
		}
	}

	return sb.String()
}
