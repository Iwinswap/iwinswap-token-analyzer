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
// VolumeAnalyzer component. It verifies the entire lifecycle in a sequential flow
// to prevent test-induced race conditions.
func TestVolumeAnalyzer_Lifecycle(t *testing.T) {
	// --- Test Configuration ---
	expiryCheckFrequency := 50 * time.Millisecond
	recordStaleDuration := 100 * time.Millisecond

	// --- Step 1: Initialization ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure shutdown is called at the end of the test.

	analyzer := NewVolumeAnalyzer(ctx, expiryCheckFrequency, recordStaleDuration)
	require.NotNil(t, analyzer, "Constructor should not return nil")

	// --- Step 2: Initial Update and Verification ---
	t.Log("Lifecycle Step: Initial Update")
	initialLogs := []types.Log{
		createTransferLog(t, testToken1, testWalletA, testWalletC, 100),
		createTransferLog(t, testToken1, testWalletB, testWalletC, 500),
		createTransferLog(t, testToken1, testWalletA, testWalletC, 200),
	}
	analyzer.Update(initialLogs)

	holders := analyzer.TokenByMaxKnownHolder()
	require.Len(t, holders, 1, "Should have one token record after initial update")
	assert.Equal(t, testWalletB, holders[testToken1], "WalletB should be the initial max holder")

	// --- Step 3: Record Expiry Verification ---
	t.Log("Lifecycle Step: Verifying Record Expiry")
	waitDuration := recordStaleDuration + expiryCheckFrequency
	assert.Eventually(t, func() bool {
		return len(analyzer.TokenByMaxKnownHolder()) == 0
	}, waitDuration*2, expiryCheckFrequency/2, "Records map should be empty after expiry")

	// --- Step 4: State Update After Expiry Verification ---
	t.Log("Lifecycle Step: Verifying Update After Expiry")
	secondLogs := []types.Log{
		// FIX: Corrected the function call with the right arguments.
		// The original call was missing the 'to' address and passed the amount
		// in its place, causing a type mismatch.
		createTransferLog(t, testToken1, testWalletC, testWalletD, 9999),
	}
	analyzer.Update(secondLogs)

	holdersAfterUpdate := analyzer.TokenByMaxKnownHolder()
	require.Len(t, holdersAfterUpdate, 1, "Should have one token record after second update")
	assert.Equal(t, testWalletC, holdersAfterUpdate[testToken1], "WalletC should be the new max holder")

	// --- Step 5: Graceful Shutdown Verification ---
	t.Log("Lifecycle Step: Verifying Graceful Shutdown")
	assert.NotPanics(t, func() {
		analyzer.Stop()
		analyzer.Stop() // Calling it again should not panic.
	}, "Calling Stop multiple times should not cause a panic")
}

// TestVolumeAnalyzer_ConcurrencyAndStress puts the VolumeAnalyzer under heavy load
// with concurrent reads and writes to ensure its state management is robust.
func TestVolumeAnalyzer_ConcurrencyAndStress(t *testing.T) {
	// --- Test Configuration ---
	expiryCheckFrequency := 200 * time.Millisecond
	recordStaleDuration := 1 * time.Second

	// --- Step 1: Initialization ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	analyzer := NewVolumeAnalyzer(ctx, expiryCheckFrequency, recordStaleDuration)
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
				logs := []types.Log{
					createTransferLog(t, testToken1, testWalletA, testWalletC, 10),
					createTransferLog(t, testToken1, testWalletB, testWalletD, 20),
					createTransferLog(t, testToken2, testWalletC, testWalletD, 5),
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

	expectedHolderToken1 := testWalletB
	expectedHolderToken2 := testWalletC

	require.Len(t, finalHolders, 2, "Should have records for exactly two tokens")
	assert.Equal(t, expectedHolderToken1, finalHolders[testToken1], "Incorrect final max holder for token 1")
	assert.Equal(t, expectedHolderToken2, finalHolders[testToken2], "Incorrect final max holder for token 2")

	// --- Step 4: Verify Expiry Still Works Under Load ---
	t.Logf("Waiting for all records to become stale (waiting > %s)...", recordStaleDuration)
	assert.Eventually(t, func() bool {
		return len(analyzer.TokenByMaxKnownHolder()) == 0
	}, recordStaleDuration*2, expiryCheckFrequency/4, "Records map should be empty after expiry under load")
}
