package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AccountState struct {
	TotalBalance          float64 `json:"total_balance"`
	AvailableBalance      float64 `json:"available_balance"`
	TotalUnrealizedProfit float64 `json:"total_unrealized_profit"`
	PositionCount         int     `json:"position_count"`
	MarginUsedPct         float64 `json:"margin_used_pct"`
	InitialBalance        float64 `json:"initial_balance"`
}

type Decision struct {
	Action    string    `json:"action"`
	Symbol    string    `json:"symbol"`
	Quantity  float64   `json:"quantity"`
	Leverage  int       `json:"leverage"`
	Price     float64   `json:"price"`
	OrderID   int64     `json:"order_id"`
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
	Error     string    `json:"error"`
}

type DecisionLog struct {
	Timestamp           time.Time    `json:"timestamp"`
	CycleNumber         int          `json:"cycle_number"`
	SystemPrompt        string       `json:"system_prompt"`
	InputPrompt         string       `json:"input_prompt"`
	CotTrace            string       `json:"cot_trace"`
	DecisionJSON        string       `json:"decision_json"`
	AccountState        AccountState `json:"account_state"`
	Positions           interface{}  `json:"positions"`
	CandidateCoins      []string     `json:"candidate_coins"`
	Decisions           []Decision   `json:"decisions"`
	ExecutionLog        []string     `json:"execution_log"`
	Success             bool         `json:"success"`
	ErrorMessage        string       `json:"error_message"`
	AIRequestDurationMs int64        `json:"ai_request_duration_ms"`
}

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
	colorBold   = "\033[1m"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run view_decision.go <decision_log.json> [--output file.txt]")
		os.Exit(1)
	}

	filePath := os.Args[1]
	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	var log DecisionLog
	if err := json.Unmarshal(data, &log); err != nil {
		fmt.Printf("Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	// 检查是否指定输出文件
	var outputFile string
	if len(os.Args) >= 4 && os.Args[2] == "--output" {
		outputFile = os.Args[3]
	} else {
		// 自动生成输出文件名
		dir := filepath.Dir(filePath)
		base := filepath.Base(filePath)
		outputFile = filepath.Join(dir, strings.TrimSuffix(base, ".json")+"_report.txt")
	}

	// 输出到终端（带颜色）
	printDecisionLog(log)

	// 输出到文件（纯文本）
	if err := writeToFile(log, outputFile); err != nil {
		fmt.Printf("Error writing to file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n📄 已保存详细报告到: %s\n", outputFile)
}

func printDecisionLog(log DecisionLog) {
	printSeparator("=")
	printHeader("📊 交易决策日志")
	printSeparator("=")
	fmt.Println()

	// 基本信息
	printSection("基本信息")
	fmt.Printf("  时间: %s%s%s\n", colorCyan, log.Timestamp.Format("2006-01-02 15:04:05"), colorReset)
	fmt.Printf("  周期: %s#%d%s\n", colorYellow, log.CycleNumber, colorReset)
	fmt.Printf("  AI耗时: %s%d ms%s\n", colorPurple, log.AIRequestDurationMs, colorReset)
	statusColor := colorGreen
	statusText := "✓ 成功"
	if !log.Success {
		statusColor = colorRed
		statusText = "✗ 失败"
	}
	fmt.Printf("  状态: %s%s%s\n", statusColor, statusText, colorReset)
	if log.ErrorMessage != "" {
		fmt.Printf("  错误: %s%s%s\n", colorRed, log.ErrorMessage, colorReset)
	}
	fmt.Println()

	// 账户状态
	printSection("账户状态")
	fmt.Printf("  总权益: %s%.2f USDT%s\n", colorGreen, log.AccountState.TotalBalance, colorReset)
	fmt.Printf("  可用余额: %s%.2f USDT%s (%.1f%%)\n",
		colorCyan,
		log.AccountState.AvailableBalance,
		colorReset,
		log.AccountState.AvailableBalance/log.AccountState.TotalBalance*100)

	profitColor := colorGreen
	profitSign := "+"
	if log.AccountState.TotalUnrealizedProfit < 0 {
		profitColor = colorRed
		profitSign = ""
	}
	fmt.Printf("  未实现盈亏: %s%s%.2f USDT%s\n",
		profitColor,
		profitSign,
		log.AccountState.TotalUnrealizedProfit,
		colorReset)
	fmt.Printf("  持仓数量: %s%d%s\n", colorYellow, log.AccountState.PositionCount, colorReset)
	fmt.Printf("  保证金占用: %s%.2f%%%s\n", colorPurple, log.AccountState.MarginUsedPct, colorReset)
	fmt.Println()

	// 候选币种
	if len(log.CandidateCoins) > 0 {
		printSection("候选币种")
		for i, coin := range log.CandidateCoins {
			fmt.Printf("  %d. %s%s%s\n", i+1, colorYellow, coin, colorReset)
		}
		fmt.Println()
	}

	// AI思维链
	printSection("AI 思维链")
	printWrappedText(log.CotTrace, 2)
	fmt.Println()

	// 决策
	printSection("决策结果")
	for i, decision := range log.Decisions {
		fmt.Printf("  %s[%d] %s%s\n", colorBold, i+1, decision.Symbol, colorReset)

		actionColor := colorCyan
		actionIcon := "⏸"
		switch decision.Action {
		case "open_long":
			actionColor = colorGreen
			actionIcon = "📈"
		case "open_short":
			actionColor = colorRed
			actionIcon = "📉"
		case "close_long", "close_short":
			actionColor = colorYellow
			actionIcon = "🔒"
		case "wait":
			actionColor = colorWhite
			actionIcon = "⏳"
		}

		fmt.Printf("    操作: %s%s %s%s\n", actionColor, actionIcon, decision.Action, colorReset)

		if decision.Leverage > 0 {
			fmt.Printf("    杠杆: %s%dx%s\n", colorPurple, decision.Leverage, colorReset)
		}
		if decision.Quantity > 0 {
			fmt.Printf("    数量: %s%.4f%s\n", colorCyan, decision.Quantity, colorReset)
		}
		if decision.Price > 0 {
			fmt.Printf("    价格: %s%.2f%s\n", colorYellow, decision.Price, colorReset)
		}

		successColor := colorGreen
		successText := "✓"
		if !decision.Success {
			successColor = colorRed
			successText = "✗"
		}
		fmt.Printf("    执行: %s%s%s\n", successColor, successText, colorReset)

		if decision.Error != "" {
			fmt.Printf("    错误: %s%s%s\n", colorRed, decision.Error, colorReset)
		}
		fmt.Println()
	}

	// 执行日志
	if len(log.ExecutionLog) > 0 {
		printSection("执行日志")
		for _, logLine := range log.ExecutionLog {
			icon := "  •"
			if strings.Contains(logLine, "✓") || strings.Contains(logLine, "成功") {
				fmt.Printf("  %s%s%s\n", colorGreen, logLine, colorReset)
			} else if strings.Contains(logLine, "✗") || strings.Contains(logLine, "失败") {
				fmt.Printf("  %s%s%s\n", colorRed, logLine, colorReset)
			} else {
				fmt.Printf("  %s %s\n", icon, logLine)
			}
		}
		fmt.Println()
	}

	printSeparator("=")
}

func printHeader(text string) {
	fmt.Printf("%s%s%s%s%s\n", colorBold, colorCyan, text, colorReset, colorReset)
}

func printSection(title string) {
	fmt.Printf("%s%s▶ %s%s\n", colorBold, colorBlue, title, colorReset)
}

func printSeparator(char string) {
	fmt.Println(strings.Repeat(char, 80))
}

func printWrappedText(text string, indent int) {
	indentStr := strings.Repeat(" ", indent)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		fmt.Printf("%s%s\n", indentStr, line)
	}
}

// writeToFile 将决策日志写入文件（纯文本格式）
func writeToFile(log DecisionLog, filePath string) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	w := file

	// 标题
	writeLine(w, strings.Repeat("=", 100))
	writeLine(w, centerText("交易决策日志详细报告", 100))
	writeLine(w, strings.Repeat("=", 100))
	writeLine(w, "")

	// 基本信息
	writeSection(w, "基本信息")
	writeLine(w, fmt.Sprintf("  时间: %s", log.Timestamp.Format("2006-01-02 15:04:05")))
	writeLine(w, fmt.Sprintf("  周期: #%d", log.CycleNumber))
	writeLine(w, fmt.Sprintf("  AI耗时: %d ms (%.2f秒)", log.AIRequestDurationMs, float64(log.AIRequestDurationMs)/1000))
	statusText := "成功 ✓"
	if !log.Success {
		statusText = "失败 ✗"
	}
	writeLine(w, fmt.Sprintf("  状态: %s", statusText))
	if log.ErrorMessage != "" {
		writeLine(w, fmt.Sprintf("  错误: %s", log.ErrorMessage))
	}
	writeLine(w, "")

	// 账户状态
	writeSection(w, "账户状态")
	writeLine(w, fmt.Sprintf("  总权益: %.2f USDT", log.AccountState.TotalBalance))
	writeLine(w, fmt.Sprintf("  可用余额: %.2f USDT (%.1f%%)",
		log.AccountState.AvailableBalance,
		log.AccountState.AvailableBalance/log.AccountState.TotalBalance*100))

	profitSign := "+"
	if log.AccountState.TotalUnrealizedProfit < 0 {
		profitSign = ""
	}
	writeLine(w, fmt.Sprintf("  未实现盈亏: %s%.2f USDT", profitSign, log.AccountState.TotalUnrealizedProfit))
	writeLine(w, fmt.Sprintf("  持仓数量: %d", log.AccountState.PositionCount))
	writeLine(w, fmt.Sprintf("  保证金占用: %.2f%%", log.AccountState.MarginUsedPct))
	writeLine(w, "")

	// 候选币种
	if len(log.CandidateCoins) > 0 {
		writeSection(w, "候选币种")
		for i, coin := range log.CandidateCoins {
			writeLine(w, fmt.Sprintf("  %d. %s", i+1, coin))
		}
		writeLine(w, "")
	}

	// System Prompt
	writeSection(w, "系统提示词 (System Prompt)")
	writeLine(w, strings.Repeat("-", 100))
	writeWrappedTextToFile(w, log.SystemPrompt, 2)
	writeLine(w, strings.Repeat("-", 100))
	writeLine(w, "")

	// Input Prompt
	writeSection(w, "输入提示词 (Input Prompt)")
	writeLine(w, strings.Repeat("-", 100))
	writeWrappedTextToFile(w, log.InputPrompt, 2)
	writeLine(w, strings.Repeat("-", 100))
	writeLine(w, "")

	// AI思维链
	writeSection(w, "AI 思维链分析 (Chain of Thought)")
	writeLine(w, strings.Repeat("-", 100))
	writeWrappedTextToFile(w, log.CotTrace, 2)
	writeLine(w, strings.Repeat("-", 100))
	writeLine(w, "")

	// 决策JSON
	writeSection(w, "原始决策 JSON")
	writeLine(w, strings.Repeat("-", 100))
	// 格式化JSON
	var prettyJSON interface{}
	if err := json.Unmarshal([]byte(log.DecisionJSON), &prettyJSON); err == nil {
		formatted, _ := json.MarshalIndent(prettyJSON, "  ", "  ")
		writeWrappedTextToFile(w, string(formatted), 2)
	} else {
		writeWrappedTextToFile(w, log.DecisionJSON, 2)
	}
	writeLine(w, strings.Repeat("-", 100))
	writeLine(w, "")

	// 决策结果
	writeSection(w, "决策结果")
	for i, decision := range log.Decisions {
		writeLine(w, fmt.Sprintf("  [%d] %s", i+1, decision.Symbol))

		actionIcon := ""
		switch decision.Action {
		case "open_long":
			actionIcon = "📈"
		case "open_short":
			actionIcon = "📉"
		case "close_long", "close_short":
			actionIcon = "🔒"
		case "wait":
			actionIcon = "⏳"
		case "hold":
			actionIcon = "⏸"
		}

		writeLine(w, fmt.Sprintf("    操作: %s %s", actionIcon, decision.Action))

		if decision.Leverage > 0 {
			writeLine(w, fmt.Sprintf("    杠杆: %dx", decision.Leverage))
		}
		if decision.Quantity > 0 {
			writeLine(w, fmt.Sprintf("    数量: %.4f", decision.Quantity))
		}
		if decision.Price > 0 {
			writeLine(w, fmt.Sprintf("    价格: %.2f", decision.Price))
		}

		successText := "✓"
		if !decision.Success {
			successText = "✗"
		}
		writeLine(w, fmt.Sprintf("    执行: %s", successText))

		if decision.Error != "" {
			writeLine(w, fmt.Sprintf("    错误: %s", decision.Error))
		}
		writeLine(w, "")
	}

	// 执行日志
	if len(log.ExecutionLog) > 0 {
		writeSection(w, "执行日志")
		for _, logLine := range log.ExecutionLog {
			writeLine(w, fmt.Sprintf("  • %s", logLine))
		}
		writeLine(w, "")
	}

	writeLine(w, strings.Repeat("=", 100))
	writeLine(w, centerText("报告结束", 100))
	writeLine(w, strings.Repeat("=", 100))

	return nil
}

func writeLine(w io.Writer, text string) {
	fmt.Fprintln(w, text)
}

func writeSection(w io.Writer, title string) {
	fmt.Fprintf(w, "\n▶ %s\n\n", strings.ToUpper(title))
}

func writeWrappedTextToFile(w io.Writer, text string, indent int) {
	indentStr := strings.Repeat(" ", indent)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		fmt.Fprintf(w, "%s%s\n", indentStr, line)
	}
}

func centerText(text string, width int) string {
	if len(text) >= width {
		return text
	}
	padding := (width - len(text)) / 2
	return strings.Repeat(" ", padding) + text
}
