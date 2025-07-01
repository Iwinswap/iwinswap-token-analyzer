package logextractor

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLiveExtractor_Run_HappyPath verifies the core success path where blocks are
// processed successfully.
func TestLiveExtractor_Run_HappyPath(t *testing.T) {
	// --- Setup ---
	const numberOfBlocksToEmit = 10
	var processingWg sync.WaitGroup
	processingWg.Add(numberOfBlocksToEmit)

	// Setup Handlers (for the Run method)
	var capturedErrs []error
	var errsMu sync.Mutex
	errorHandler := func(err error) {
		errsMu.Lock()
		capturedErrs = append(capturedErrs, err)
		errsMu.Unlock()
	}
	logsHandler := func(ctx context.Context, logs []types.Log) error {
		processingWg.Done()
		return nil
	}

	// Setup Dependencies (for the constructor)
	testBloom := func(types.Bloom) bool { return true }
	getFilterer := func() (func(context.Context, ethereum.FilterQuery) ([]types.Log, error), error) {
		return func(context.Context, ethereum.FilterQuery) ([]types.Log, error) { return []types.Log{{}}, nil }, nil
	}

	extractor, err := NewLiveExtractor(testBloom, getFilterer)
	require.NoError(t, err)

	// --- Execution ---
	newBlockEventCh := make(chan *types.Block, numberOfBlocksToEmit)
	ctx, cancel := context.WithCancel(context.Background())
	var lifecycleWg sync.WaitGroup
	lifecycleWg.Add(1)
	go func() {
		defer lifecycleWg.Done()
		extractor.Run(ctx, newBlockEventCh, logsHandler, errorHandler)
	}()

	for i := 1; i <= numberOfBlocksToEmit; i++ {
		newBlockEventCh <- types.NewBlockWithHeader(&types.Header{Number: big.NewInt(int64(i))})
	}

	processingWg.Wait()
	cancel()
	lifecycleWg.Wait()

	// --- Verification ---
	assert.Empty(t, capturedErrs, "ErrorHandler should not have been called")
}

// TestLiveExtractor_Run_Scenarios tests all edge cases and failure modes for the Run method.
func TestLiveExtractor_Run_Scenarios(t *testing.T) {

	// Scenario: Block is correctly skipped if the bloom filter does not match.
	t.Run("skips block when bloom filter returns false", func(t *testing.T) {
		// --- Setup ---
		var logsHandlerCalled, filtererFactoryCalled atomic.Bool
		// Handlers
		errorHandler := func(err error) { t.Errorf("ErrorHandler called unexpectedly: %v", err) }
		logsHandler := func(context.Context, []types.Log) error {
			logsHandlerCalled.Store(true)
			return nil
		}
		// Dependencies
		testBloom := func(types.Bloom) bool { return false } // The key part of this test
		getFilterer := func() (func(context.Context, ethereum.FilterQuery) ([]types.Log, error), error) {
			filtererFactoryCalled.Store(true)
			return nil, nil // This function should never be called
		}

		extractor, err := NewLiveExtractor(testBloom, getFilterer)
		require.NoError(t, err)

		// --- Execution ---
		newBlockEventCh := make(chan *types.Block, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		go extractor.Run(ctx, newBlockEventCh, logsHandler, errorHandler)
		newBlockEventCh <- types.NewBlockWithHeader(&types.Header{Number: big.NewInt(1)})
		<-ctx.Done() // Wait for test to time out

		// --- Verification ---
		assert.False(t, logsHandlerCalled.Load(), "LogsHandler should not be called")
		assert.False(t, filtererFactoryCalled.Load(), "getFilterer should not be called")
	})

	// Scenario: A nil block is received from the channel.
	t.Run("handles nil block gracefully", func(t *testing.T) {
		// --- Setup ---
		var wg sync.WaitGroup
		wg.Add(1)
		var errReceived error
		// Handlers
		errorHandler := func(err error) {
			errReceived = err
			wg.Done()
		}
		logsHandler := func(context.Context, []types.Log) error {
			t.Error("LogsHandler should not be called")
			return nil
		}
		// Dependencies (must be valid but designed to fail test if called)
		testBloom := func(types.Bloom) bool {
			t.Error("TestBloom should not be called for nil block")
			return false
		}
		extractor, err := NewLiveExtractor(testBloom, func() (func(context.Context, ethereum.FilterQuery) ([]types.Log, error), error) { return nil, nil })
		require.NoError(t, err)

		// --- Execution ---
		newBlockEventCh := make(chan *types.Block, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go extractor.Run(ctx, newBlockEventCh, logsHandler, errorHandler)
		newBlockEventCh <- nil
		wg.Wait()

		// --- Verification ---
		require.Error(t, errReceived)
		assert.Contains(t, errReceived.Error(), "received nil block")
	})

	// Scenario: The factory function for getting a log filterer fails.
	t.Run("handles error from getFilterer", func(t *testing.T) {
		// --- Setup ---
		var wg sync.WaitGroup
		wg.Add(1)
		var errReceived error
		expectedErr := errors.New("network connection failed")
		// Handlers
		errorHandler := func(err error) {
			errReceived = err
			wg.Done()
		}
		logsHandler := func(context.Context, []types.Log) error {
			t.Error("LogsHandler should not be called")
			return nil
		}
		// Dependencies
		getFilterer := func() (func(context.Context, ethereum.FilterQuery) ([]types.Log, error), error) {
			return nil, expectedErr
		}
		extractor, err := NewLiveExtractor(func(types.Bloom) bool { return true }, getFilterer)
		require.NoError(t, err)

		// --- Execution ---
		newBlockEventCh := make(chan *types.Block, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go extractor.Run(ctx, newBlockEventCh, logsHandler, errorHandler)
		newBlockEventCh <- types.NewBlockWithHeader(&types.Header{Number: big.NewInt(1)})
		wg.Wait()

		// --- Verification ---
		assert.Equal(t, expectedErr, errReceived)
	})

	// Scenario: The returned filterer function itself fails (e.g., the eth_getLogs call fails).
	t.Run("handles error from filterer function", func(t *testing.T) {
		// --- Setup ---
		var wg sync.WaitGroup
		wg.Add(1)
		var errReceived error
		expectedErr := errors.New("eth_getLogs RPC error")
		// Handlers
		errorHandler := func(err error) {
			errReceived = err
			wg.Done()
		}
		logsHandler := func(context.Context, []types.Log) error {
			t.Error("LogsHandler should not be called")
			return nil
		}
		// Dependencies
		getFilterer := func() (func(context.Context, ethereum.FilterQuery) ([]types.Log, error), error) {
			return func(context.Context, ethereum.FilterQuery) ([]types.Log, error) { return nil, expectedErr }, nil
		}
		extractor, err := NewLiveExtractor(func(types.Bloom) bool { return true }, getFilterer)
		require.NoError(t, err)

		// --- Execution ---
		newBlockEventCh := make(chan *types.Block, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go extractor.Run(ctx, newBlockEventCh, logsHandler, errorHandler)
		newBlockEventCh <- types.NewBlockWithHeader(&types.Header{Number: big.NewInt(1)})
		wg.Wait()

		// --- Verification ---
		assert.Equal(t, expectedErr, errReceived)
	})

	// Scenario: The final log handler fails (e.g., a database write error).
	t.Run("handles error from LogsHandler", func(t *testing.T) {
		// --- Setup ---
		var wg sync.WaitGroup
		wg.Add(1)
		var errReceived error
		expectedErr := errors.New("failed to write to database")
		// Handlers
		errorHandler := func(err error) {
			errReceived = err
			wg.Done()
		}
		logsHandler := func(context.Context, []types.Log) error { return expectedErr }
		// Dependencies
		getFilterer := func() (func(context.Context, ethereum.FilterQuery) ([]types.Log, error), error) {
			return func(context.Context, ethereum.FilterQuery) ([]types.Log, error) { return []types.Log{{}}, nil }, nil
		}
		extractor, err := NewLiveExtractor(func(types.Bloom) bool { return true }, getFilterer)
		require.NoError(t, err)

		// --- Execution ---
		newBlockEventCh := make(chan *types.Block, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go extractor.Run(ctx, newBlockEventCh, logsHandler, errorHandler)
		newBlockEventCh <- types.NewBlockWithHeader(&types.Header{Number: big.NewInt(1)})
		wg.Wait()

		// --- Verification ---
		assert.Equal(t, expectedErr, errReceived)
	})

	// Scenario: The listener continues to process new blocks after a recoverable error.
	t.Run("continues processing after a recoverable error", func(t *testing.T) {
		// --- Setup ---
		var wg sync.WaitGroup
		wg.Add(2) // We expect two blocks to be fully processed (one failure, one success).
		var errReceived error
		var successCount atomic.Int32
		// Handlers
		errorHandler := func(err error) {
			errReceived = err
			wg.Done() // Signal that the error path for block 1 is complete.
		}
		logsHandler := func(ctx context.Context, logs []types.Log) error {
			if logs[0].BlockNumber == 1 {
				return errors.New("transient error on block 1")
			}
			successCount.Add(1)
			wg.Done() // Signal that the success path for block 2 is complete.
			return nil
		}
		// Dependencies
		getFilterer := func() (func(context.Context, ethereum.FilterQuery) ([]types.Log, error), error) {
			return func(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
				return []types.Log{{BlockNumber: q.FromBlock.Uint64()}}, nil
			}, nil
		}
		extractor, err := NewLiveExtractor(func(types.Bloom) bool { return true }, getFilterer)
		require.NoError(t, err)

		// --- Execution ---
		newBlockEventCh := make(chan *types.Block, 2)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go extractor.Run(ctx, newBlockEventCh, logsHandler, errorHandler)

		newBlockEventCh <- types.NewBlockWithHeader(&types.Header{Number: big.NewInt(1)})
		newBlockEventCh <- types.NewBlockWithHeader(&types.Header{Number: big.NewInt(2)})
		wg.Wait() // Wait for both blocks to finish their lifecycle.

		// --- Verification ---
		require.Error(t, errReceived, "An error should have been captured")
		assert.Contains(t, errReceived.Error(), "transient error on block 1")
		assert.Equal(t, int32(1), successCount.Load(), "Should have successfully processed the second block after the first one failed")
	})

	// Scenario: The listener shuts down gracefully when its context is cancelled.
	t.Run("shuts down gracefully on context cancellation", func(t *testing.T) {
		// --- Setup ---
		var lifecycleWg sync.WaitGroup
		lifecycleWg.Add(1)
		// Provide minimal valid dependencies to satisfy the constructor.
		extractor, err := NewLiveExtractor(
			func(types.Bloom) bool { return false },
			func() (func(context.Context, ethereum.FilterQuery) ([]types.Log, error), error) {
				return nil, errors.New("not implemented")
			},
		)
		require.NoError(t, err)

		newBlockEventCh := make(chan *types.Block)
		ctx, cancel := context.WithCancel(context.Background())

		// --- Execution ---
		go func() {
			defer lifecycleWg.Done()
			// The handlers can be nil because they won't be called before shutdown.
			extractor.Run(ctx, newBlockEventCh, nil, nil)
		}()

		cancel() // Immediately cancel the context.

		// --- Verification ---
		done := make(chan struct{})
		go func() {
			lifecycleWg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Success
		case <-time.After(1 * time.Second):
			t.Fatal("Run method did not return after context cancellation")
		}
	})
}
