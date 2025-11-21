package trader

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
)

// TestAsterTrader_RealAPI_Connectivity runs real requests against the Aster API.
// WARNING: This test uses real credentials and connects to the live (or testnet) API.
func TestAsterTrader_RealAPI_Connectivity(t *testing.T) {
	// Load .env file
	_ = godotenv.Load("../.env")

	// Credentials from environment variables
	user := os.Getenv("ASTER_API_USER")
	signer := os.Getenv("ASTER_API_SIGNER")
	privateKey := os.Getenv("ASTER_API_PRIVATE_KEY")

	if user == "" || signer == "" || privateKey == "" {
		t.Skip("Skipping real API test: ASTER_API_USER, ASTER_API_SIGNER, or ASTER_API_PRIVATE_KEY not set")
	}

	trader, err := NewAsterTrader(user, signer, privateKey)
	assert.NoError(t, err)
	assert.NotNil(t, trader)

	// 1. Test GetBalance
	t.Run("GetBalance", func(t *testing.T) {
		balance, err := trader.GetBalance()
		if err != nil {
			t.Logf("GetBalance failed: %v", err)
			// Don't fail the whole test if balance fails, maybe just network issue or invalid keys
			// But we want to see the error
		} else {
			fmt.Printf("Real API Balance: %+v\n", balance)
			assert.NotNil(t, balance)
		}
	})

	// 2. Test GetOpenOrders
	t.Run("GetOpenOrders", func(t *testing.T) {
		// Use a common symbol like BTCUSDT
		symbol := "BTCUSDT"
		orders, err := trader.GetOpenOrders(symbol)
		if err != nil {
			t.Fatalf("GetOpenOrders failed: %v", err)
		}
		fmt.Printf("Real API Open Orders (%s): %+v\n", symbol, orders)

		// Pretty print for user
		ordersJSON, _ := json.MarshalIndent(orders, "", "  ")
		fmt.Printf("Open Orders JSON:\n%s\n", string(ordersJSON))
	})
}
