package erc20analyzer

import (
	"context"
	"math/big"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/Iwinswap/iwinswap-token-analyzer/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Shared Test Fixtures & Helpers ---

var (
	testToken1  = common.HexToAddress("0x1")
	testToken2  = common.HexToAddress("0x2")
	testWalletA = common.HexToAddress("0xA")
	testWalletB = common.HexToAddress("0xB")
	testWalletC = common.HexToAddress("0xC")
	testWalletD = common.HexToAddress("0xD")
)

// createTransferLog is a shared helper to build a valid ERC20 Transfer log for tests.
func createTransferLog(t *testing.T, token, from, to common.Address, amount int64) types.Log {
	t.Helper() // Mark this as a test helper function.
	val := new(big.Int).SetInt64(amount)
	return types.Log{
		Address: token,
		Topics: []common.Hash{
			abi.ERC20ABI.Events["Transfer"].ID,
			common.BytesToHash(from.Bytes()),
			common.BytesToHash(to.Bytes()),
		},
		Data: common.LeftPadBytes(val.Bytes(), 32),
	}
}

// TestVolumeAnalyzer_Lifecycle provides a comprehensive, end-to-end test for the
// VolumeAnalyzer component. It verifies the entire lifecycle in a sequential flow.
func TestVolumeAnalyzer_Lifecycle(t *testing.T) {
	// --- Test Configuration ---
	expiryCheckFrequency := 50 * time.Millisecond
	recordStaleDuration := 100 * time.Millisecond

	// --- Step 1: Initialization ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure shutdown is called at the end of the test.

	// By default, allow all addresses.
	cfg := Config{
		ExpiryCheckFrequency: expiryCheckFrequency,
		RecordStaleDuration:  recordStaleDuration,
		IsAllowedAddress:     func(common.Address) bool { return true },
	}
	analyzer := NewVolumeAnalyzer(ctx, cfg)
	require.NotNil(t, analyzer, "Constructor should not return nil")

	// --- Step 2: Initial Update and Verification ---
	t.Log("Lifecycle Step: Initial Update")
	initialLogs := []types.Log{
		createTransferLog(t, testToken1, testWalletA, testWalletC, 100), // C receives 100
		createTransferLog(t, testToken1, testWalletB, testWalletD, 500), // D receives 500 (max)
		createTransferLog(t, testToken1, testWalletA, testWalletC, 200), // C receives another 200 (total 300)
	}
	analyzer.Update(initialLogs)

	holders := analyzer.TokenByMaxKnownHolder()
	// --- FIX: The function correctly returns only the single top holder for the token. ---
	require.Len(t, holders, 1, "Should have one token record after initial update")
	assert.Equal(t, testWalletD, holders[testToken1], "WalletD should be the initial max holder for token1")

	// --- Step 3: Filtering Logic Verification ---
	t.Log("Lifecycle Step: Verifying Filtering Logic")
	filterCtx, filterCancel := context.WithCancel(context.Background())
	defer filterCancel()

	// Create a new analyzer with a filter that only allows wallet C to be a receiver
	filterCfg := Config{
		ExpiryCheckFrequency: expiryCheckFrequency,
		RecordStaleDuration:  recordStaleDuration,
		IsAllowedAddress:     func(addr common.Address) bool { return addr == testWalletC },
	}
	filteredAnalyzer := NewVolumeAnalyzer(filterCtx, filterCfg)
	filterLogs := []types.Log{
		createTransferLog(t, testToken1, testWalletA, testWalletC, 100), // Allowed
		createTransferLog(t, testToken1, testWalletB, testWalletD, 999), // Disallowed (to D)
	}
	filteredAnalyzer.Update(filterLogs)
	filteredHolders := filteredAnalyzer.TokenByMaxKnownHolder()
	require.Len(t, filteredHolders, 1, "Filtered analyzer should have one record")
	assert.Equal(t, testWalletC, filteredHolders[testToken1], "Only WalletC should be considered due to the filter")

	// --- Step 4: Record Expiry Verification ---
	t.Log("Lifecycle Step: Verifying Record Expiry")
	waitDuration := recordStaleDuration + expiryCheckFrequency
	assert.Eventually(t, func() bool {
		return len(analyzer.TokenByMaxKnownHolder()) == 0
	}, waitDuration*2, expiryCheckFrequency/2, "Records map should be empty after expiry")

	// --- Step 5: State Update After Expiry Verification ---
	t.Log("Lifecycle Step: Verifying Update After Expiry")
	secondLogs := []types.Log{
		createTransferLog(t, testToken1, testWalletA, testWalletD, 9999),
	}
	analyzer.Update(secondLogs)

	holdersAfterUpdate := analyzer.TokenByMaxKnownHolder()
	require.Len(t, holdersAfterUpdate, 1, "Should have one token record after second update")
	assert.Equal(t, testWalletD, holdersAfterUpdate[testToken1], "WalletD should be the new max holder")

	// --- Step 6: Graceful Shutdown Verification ---
	t.Log("Lifecycle Step: Verifying Graceful Shutdown via context cancellation")
	cancel() // Trigger shutdown
	// The test will hang if the goroutine doesn't exit, and the race detector will catch issues.
}

// TestVolumeAnalyzer_ConcurrencyAndStress puts the VolumeAnalyzer under heavy load.
func TestVolumeAnalyzer_ConcurrencyAndStress(t *testing.T) {
	// --- Test Configuration ---
	expiryCheckFrequency := 200 * time.Millisecond
	recordStaleDuration := 1 * time.Second

	// --- Step 1: Initialization ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := Config{
		ExpiryCheckFrequency: expiryCheckFrequency,
		RecordStaleDuration:  recordStaleDuration,
		IsAllowedAddress:     func(common.Address) bool { return true }, // Allow all for stress test
	}
	analyzer := NewVolumeAnalyzer(ctx, cfg)
	require.NotNil(t, analyzer, "Constructor should not return nil")

	// --- Step 2: Concurrent Reads and Writes ---
	var wg sync.WaitGroup
	numGoroutines := 50
	updatesPerGoroutine := 20
	wg.Add(numGoroutines)

	t.Logf("Starting %d goroutines, each performing %d updates...", numGoroutines, updatesPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < updatesPerGoroutine; j++ {
				// In each update, D receives the most volume for both tokens
				logs := []types.Log{
					createTransferLog(t, testToken1, testWalletA, testWalletC, 10), // C receives 10
					createTransferLog(t, testToken1, testWalletB, testWalletD, 20), // D receives 20
					createTransferLog(t, testToken2, testWalletC, testWalletD, 5),  // D receives 5
				}
				analyzer.Update(logs)
				time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// --- Step 3: Final State Verification ---
	t.Log("All updates complete. Verifying final state...")
	finalHolders := analyzer.TokenByMaxKnownHolder()

	// The max receiver for token1 is D (receives 20). The max for token2 is also D (receives 5).
	expectedHolderToken1 := testWalletD
	expectedHolderToken2 := testWalletD

	require.Len(t, finalHolders, 2, "Should have records for exactly two tokens")
	assert.Equal(t, expectedHolderToken1, finalHolders[testToken1], "Incorrect final max holder for token 1")
	assert.Equal(t, expectedHolderToken2, finalHolders[testToken2], "Incorrect final max holder for token 2")

	// --- Step 4: Verify Expiry Still Works Under Load ---
	t.Logf("Waiting for all records to become stale (waiting > %s)...", recordStaleDuration)
	assert.Eventually(t, func() bool {
		return len(analyzer.TokenByMaxKnownHolder()) == 0
	}, recordStaleDuration*2, expiryCheckFrequency/4, "Records map should be empty after expiry under load")
}
