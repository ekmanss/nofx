package decision

import (
	"os"
	"testing"
	"time"

	"nofx/market"
	"nofx/mcp"
)

// captureAIClient 拦截 system/user prompt，避免真正调用大模型。
type captureAIClient struct {
	systemPrompt string
	userPrompt   string
}

func (c *captureAIClient) SetAPIKey(_ string, _ string, _ string) {}
func (c *captureAIClient) SetTimeout(_ time.Duration)             {}
func (c *captureAIClient) CallWithMessages(systemPrompt, userPrompt string) (string, error) {
	c.systemPrompt = systemPrompt
	c.userPrompt = userPrompt
	return "[]", nil // 返回最小合法JSON，方便解析流程走通
}
func (c *captureAIClient) CallWithRequest(req *mcp.Request) (string, error) {
	return "[]", nil
}

// go test ./decision -run TestDumpUserPromptFromGetFullDecisionWithCustomPrompt -v
func TestDumpUserPromptFromGetFullDecisionWithCustomPrompt(t *testing.T) {
	// 确保 WSMonitorCli 非空，避免 market.Get 中空指针；不会真正连上WS。
	market.NewWSMonitor(10)

	ctx := &Context{
		CurrentTime: time.Now().Format(time.RFC3339),
		CallCount:   1,
		Account: AccountInfo{
			TotalEquity: 1000, // 任意示例值
		},
		CandidateCoins: []CandidateCoin{
			{Symbol: "BTCUSDT", Sources: []string{"manual"}},
		},
		Positions:       nil,
		BTCETHLeverage:  1,
		AltcoinLeverage: 1,
	}

	client := &captureAIClient{}

	decision, err := GetFullDecisionWithCustomPrompt(ctx, client, "", false, "")
	if err != nil {
		t.Fatalf("GetFullDecisionWithCustomPrompt failed: %v", err)
	}
	if decision == nil {
		t.Fatalf("decision is nil")
	}

	if err := os.WriteFile("user_prompt_output.txt", []byte(decision.UserPrompt), 0644); err != nil {
		t.Fatalf("write user prompt failed: %v", err)
	}

	t.Logf("user prompt written to user_prompt_output.txt (length=%d)", len(decision.UserPrompt))
}
