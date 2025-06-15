package erc20analyzer

import (
	"context"
	"sync"
	"time"

	"github.com/Iwinswap/iwinswap-token-analyzer/internal/erc20analyzer"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// VolumeAnalyzer is an implementation of TokenHolderAnalyzer that determines
// the primary holder based on the highest total transfer volume.
type VolumeAnalyzer struct {
	mu            sync.RWMutex
	records       map[common.Address]MaxTransferrer
	stopChan      chan struct{}
	staleDuration time.Duration
	checkTicker   *time.Ticker
	stopOnce      sync.Once
}

// ensures we implement the correct interface
var _ erc20analyzer.TokenHolderAnalyzer = (*VolumeAnalyzer)(nil)

// NewVolumeAnalyzer creates and starts a new instance of the volume-based analyzer.
func NewVolumeAnalyzer(ctx context.Context, expiryCheckFrequency, recordStaleDuration time.Duration) *VolumeAnalyzer {
	a := &VolumeAnalyzer{
		records:       make(map[common.Address]MaxTransferrer),
		stopChan:      make(chan struct{}),
		staleDuration: recordStaleDuration,
		checkTicker:   time.NewTicker(expiryCheckFrequency),
	}
	go a.startExpiryTicker()

	// Listen for the parent context to be done, so we can stop our ticker
	go func() {
		<-ctx.Done()
		a.Stop()
	}()

	return a
}

// Update processes logs to find the highest total volume transferrers and updates the state.
func (a *VolumeAnalyzer) Update(logs []types.Log) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(logs) == 0 {
		return
	}
	// This implementation's strategy is to use the total volume.
	newRecords := ExtractMaxTotalVolumeTransferrer(logs)
	if len(newRecords) == 0 {
		return
	}

	a.records = MergeMaxTransferrers(a.records, newRecords)
}

// TokenByMaxKnownHolder returns the current primary holders based on total volume.
func (a *VolumeAnalyzer) TokenByMaxKnownHolder() map[common.Address]common.Address {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[common.Address]common.Address, len(a.records))
	for token, record := range a.records {
		result[token] = record.Address
	}
	return result
}

// startExpiryTicker runs the background process to prune stale records.
func (a *VolumeAnalyzer) startExpiryTicker() {
	for {
		select {
		case <-a.checkTicker.C:
			a.expireRecords()
		case <-a.stopChan:
			return
		}
	}
}

// expireRecords performs the actual pruning of stale data.
func (a *VolumeAnalyzer) expireRecords() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = ExpireMaxTransferrers(a.records, a.staleDuration)
}

// Stop gracefully terminates the background expiry ticker. This function is idempotent
// and safe to call multiple times.
func (a *VolumeAnalyzer) Stop() {
	a.stopOnce.Do(func() {
		a.checkTicker.Stop()
		close(a.stopChan)
	})
}
