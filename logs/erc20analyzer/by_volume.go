package erc20analyzer

import (
	"context"
	"sync"
	"time"

	"github.com/Iwinswap/iwinswap-token-analyzer/erc20analyzer"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Config holds the configuration parameters for the VolumeAnalyzer.
type Config struct {
	// ExpiryCheckFrequency is the interval at which the analyzer checks for stale records.
	ExpiryCheckFrequency time.Duration
	// RecordStaleDuration is the duration after which a record is considered stale and pruned.
	RecordStaleDuration time.Duration
	// IsAllowedAddress is a function to filter which addresses are included in the analysis.
	// If nil, all addresses are considered allowed.
	IsAllowedAddress func(common.Address) bool
}

// VolumeAnalyzer is an implementation of TokenHolderAnalyzer that determines
// the primary holder based on the highest total transfer volume.
type VolumeAnalyzer struct {
	mu               sync.RWMutex
	records          map[common.Address]MaxTransferrer
	staleDuration    time.Duration
	checkTicker      *time.Ticker
	isAllowedAddress func(common.Address) bool
}

// ensures we implement the correct interface
var _ erc20analyzer.TokenHolderAnalyzer = (*VolumeAnalyzer)(nil)

// NewVolumeAnalyzer creates and starts a new instance of the volume-based analyzer.
func NewVolumeAnalyzer(ctx context.Context, cfg Config) *VolumeAnalyzer {
	// Default to allowing all addresses if no function is provided.
	if cfg.IsAllowedAddress == nil {
		cfg.IsAllowedAddress = func(common.Address) bool { return true }
	}

	a := &VolumeAnalyzer{
		records:          make(map[common.Address]MaxTransferrer),
		staleDuration:    cfg.RecordStaleDuration,
		checkTicker:      time.NewTicker(cfg.ExpiryCheckFrequency),
		isAllowedAddress: cfg.IsAllowedAddress,
	}

	// The ticker is stopped automatically when the context is done.
	go a.startExpiryTicker(ctx)

	return a
}

// Update processes logs to find the highest total volume transferrers and updates the state.
func (a *VolumeAnalyzer) Update(logs []types.Log) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(logs) == 0 {
		return
	}

	newRecords := ExtractMaxTotalVolumeTransferrer(logs, a.isAllowedAddress)
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
// It terminates when the provided context is canceled.
func (a *VolumeAnalyzer) startExpiryTicker(ctx context.Context) {
	defer a.checkTicker.Stop()
	for {
		select {
		case <-a.checkTicker.C:
			a.expireRecords()
		case <-ctx.Done():
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
