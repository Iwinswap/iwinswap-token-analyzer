package erc20analyzer

import (
	"math/big"
	"testing"

	"github.com/Iwinswap/iwinswap-token-analyzer/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractMaxSingleTransfer verifies the logic for finding the single largest transaction.
func TestExtractMaxSingleTransfer(t *testing.T) {
	t.Parallel()

	// --- Test Fixtures (scoped to this test) ---
	var (
		token1  = common.HexToAddress("0x1")
		walletA = common.HexToAddress("0xA")
		walletB = common.HexToAddress("0xB")
		walletC = common.HexToAddress("0xC")
	)

	// createTransferLog is a helper function scoped to this test function.
	createTransferLog := func(token, from, to common.Address, amount int64) types.Log {
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

	testCases := []struct {
		name        string
		logs        []types.Log
		expectedMap map[common.Address]MaxTransferrer
		description string
	}{
		{
			name: "Happy Path - New max value updates record",
			logs: []types.Log{
				createTransferLog(token1, walletA, walletC, 500),
				createTransferLog(token1, walletB, walletC, 1000), // New max
			},
			expectedMap: map[common.Address]MaxTransferrer{
				token1: {Address: walletB, Amount: big.NewInt(1000)},
			},
			description: "Should correctly update the max transferrer when a larger transfer occurs.",
		},
		{
			name: "Edge Case - Tie in max value",
			logs: []types.Log{
				createTransferLog(token1, walletA, walletC, 1000), // WalletA is first
				createTransferLog(token1, walletB, walletC, 1000), // WalletB is second, should not update
			},
			expectedMap: map[common.Address]MaxTransferrer{
				token1: {Address: walletA, Amount: big.NewInt(1000)},
			},
			description: "In a tie, the first address to transfer the max amount should be kept.",
		},
		{
			name:        "Input - Empty log slice",
			logs:        []types.Log{},
			expectedMap: map[common.Address]MaxTransferrer{},
			description: "Should return an empty map for empty log input.",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t.Log(tc.description)
			actualMap := ExtractMaxSingleTransfer(tc.logs)
			verifyMaxTransferrerMapTestHelper(t, tc.expectedMap, actualMap)
		})
	}
}

// TestExtractMaxTotalVolumeTransferrer verifies the logic for finding the highest total volume sender.
func TestExtractMaxTotalVolumeTransferrer(t *testing.T) {
	t.Parallel()

	// --- Test Fixtures (scoped to this test) ---
	var (
		token1  = common.HexToAddress("0x1")
		token2  = common.HexToAddress("0x2")
		walletA = common.HexToAddress("0xA")
		walletB = common.HexToAddress("0xB")
		walletC = common.HexToAddress("0xC")
	)

	// Scoped helper function for this test.
	createTransferLog := func(token, from, to common.Address, amount int64) types.Log {
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

	testCases := []struct {
		name        string
		logs        []types.Log
		expectedMap map[common.Address]MaxTransferrer
		description string
	}{
		{
			name: "Happy Path - Sums transfers and finds max volume",
			logs: []types.Log{
				createTransferLog(token1, walletA, walletC, 100),
				createTransferLog(token1, walletB, walletC, 400),
				createTransferLog(token1, walletA, walletC, 301), // walletA total is now 401
				createTransferLog(token2, walletC, walletA, 999),
			},
			expectedMap: map[common.Address]MaxTransferrer{
				token1: {Address: walletA, Amount: big.NewInt(401)},
				token2: {Address: walletC, Amount: big.NewInt(999)},
			},
			description: "Should sum transfers from the same address and identify the one with the highest total volume.",
		},
		{
			name: "Edge Case - One address dominates",
			logs: []types.Log{
				createTransferLog(token1, walletA, walletC, 100),
				createTransferLog(token1, walletA, walletC, 200),
				createTransferLog(token1, walletA, walletC, 300),
			},
			expectedMap: map[common.Address]MaxTransferrer{
				token1: {Address: walletA, Amount: big.NewInt(600)},
			},
			description: "Should correctly sum up multiple transfers from a single address.",
		},
		{
			name:        "Input - Empty log slice",
			logs:        []types.Log{},
			expectedMap: map[common.Address]MaxTransferrer{},
			description: "Should return an empty map for empty log input.",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t.Log(tc.description)
			actualMap := ExtractMaxTotalVolumeTransferrer(tc.logs)
			verifyMaxTransferrerMapTestHelper(t, tc.expectedMap, actualMap)
		})
	}
}

// FuzzExtractMaxSingleTransfer ensures the function can gracefully handle arbitrary
// or malformed log data without panicking.
func FuzzExtractMaxSingleTransfer(f *testing.F) {
	// Add some valid seed data to guide the fuzzer.
	f.Add(common.HexToAddress("0x1").Bytes(), common.HexToAddress("0xA").Bytes(), int64(100))

	f.Fuzz(func(t *testing.T, addrBytes, fromBytes []byte, amount int64) {
		log := types.Log{
			Address: common.BytesToAddress(addrBytes),
			Topics: []common.Hash{
				abi.ERC20ABI.Events["Transfer"].ID, // Correctly include the event signature
				common.BytesToHash(fromBytes),
				common.HexToHash("0x0"), // Dummy recipient
			},
			Data: new(big.Int).SetInt64(amount).Bytes(),
		}
		// Test will fail if the function panics on unexpected input.
		ExtractMaxSingleTransfer([]types.Log{log})
	})
}

// FuzzExtractMaxTotalVolumeTransferrer ensures the summation logic can gracefully
// handle arbitrary log data without panicking.
func FuzzExtractMaxTotalVolumeTransferrer(f *testing.F) {
	f.Add(common.HexToAddress("0x1").Bytes(), common.HexToAddress("0xA").Bytes(), int64(100))

	f.Fuzz(func(t *testing.T, addrBytes, fromBytes []byte, amount int64) {
		log := types.Log{
			Address: common.BytesToAddress(addrBytes),
			Topics: []common.Hash{
				abi.ERC20ABI.Events["Transfer"].ID,
				common.BytesToHash(fromBytes),
				common.HexToHash("0x0"),
			},
			Data: new(big.Int).SetInt64(amount).Bytes(),
		}
		ExtractMaxTotalVolumeTransferrer([]types.Log{log})
	})
}

// verifyMaxTransferrerMapTestHelper is a file-scoped helper function to provide consistent,
// robust assertions for maps of MaxTransferrer structs.
func verifyMaxTransferrerMapTestHelper(t *testing.T, expected, actual map[common.Address]MaxTransferrer) {
	t.Helper() // Marks this function as a test helper.

	require.Len(t, actual, len(expected), "The number of tokens in the result map is incorrect.")

	for expectedToken, expectedTransferrer := range expected {
		actualTransferrer, ok := actual[expectedToken]
		require.True(t, ok, "Expected token %s was not found in the result.", expectedToken.Hex())
		assert.Equal(t, expectedTransferrer.Address, actualTransferrer.Address, "Incorrect max transferrer address for token %s", expectedToken.Hex())
		require.NotNil(t, actualTransferrer.Amount, "Actual amount should not be nil for token %s", expectedToken.Hex())
		assert.Zero(t, expectedTransferrer.Amount.Cmp(actualTransferrer.Amount), "Incorrect max transfer amount for token %s", expectedToken.Hex())
		assert.False(t, actualTransferrer.Time.IsZero(), "Timestamp was not set for token %s", expectedToken.Hex())
	}
}
