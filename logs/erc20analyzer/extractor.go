package erc20analyzer

import (
	"math/big"
	"time"

	"github.com/Iwinswap/iwinswap-token-analyzer/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// MaxTransferrer holds data about the largest transfer, including when it was seen.
type MaxTransferrer struct {
	Address common.Address
	Amount  *big.Int
	Time    time.Time
}

// --- Implementation 1: Find the Single Largest Transfer ---

// ExtractMaxSingleTransfer processes logs to find the account that performed
// the single largest transfer for each token.
func ExtractMaxSingleTransfer(logs []types.Log) map[common.Address]MaxTransferrer {
	maxTransferByToken := make(map[common.Address]MaxTransferrer)

	for _, l := range logs {
		tokenAddress := l.Address
		from, _, value, err := abi.ParseERC20TransferEvent(l)
		if err != nil {
			continue // Not a valid transfer log.
		}

		currentMax, ok := maxTransferByToken[tokenAddress]

		// If it's the first transfer for this token, or if the current transfer's
		// value is greater than the stored max, we have a new max.
		if !ok || value.Cmp(currentMax.Amount) > 0 {
			maxTransferByToken[tokenAddress] = MaxTransferrer{
				Address: from,
				Amount:  value,
				Time:    time.Now(),
			}
		}
	}
	return maxTransferByToken
}

// --- Implementation 2: Find the Highest Total Volume Transferrer ---

// ExtractMaxTotalVolumeTransferrer processes logs to find the account that transferred
// the largest total volume (sum of all their transfers) for each token.
func ExtractMaxTotalVolumeTransferrer(logs []types.Log) map[common.Address]MaxTransferrer {
	// Step 1: Aggregate the total transfer amount for every address, for every token.
	// Structure: map[tokenAddress] -> map[fromAddress] -> totalAmount
	totals := make(map[common.Address]map[common.Address]*big.Int)
	filteredLogs := FilterERC20TransferLogs(logs)
	for _, l := range filteredLogs {
		tokenAddress := l.Address
		from, _, value, err := abi.ParseERC20TransferEvent(l)
		if err != nil {
			continue
		}

		// Initialize inner map if it's the first time we see this token.
		if _, ok := totals[tokenAddress]; !ok {
			totals[tokenAddress] = make(map[common.Address]*big.Int)
		}

		// Initialize total for the address if it's the first time we see it for this token.
		if _, ok := totals[tokenAddress][from]; !ok {
			totals[tokenAddress][from] = new(big.Int)
		}

		// Add the transfer value to the running total for that address.
		totals[tokenAddress][from].Add(totals[tokenAddress][from], value)
	}

	// Step 2: Now that we have the totals, find the address with the max total for each token.
	maxTransferByToken := make(map[common.Address]MaxTransferrer)

	for token, fromMap := range totals {
		var currentMax MaxTransferrer

		for from, totalAmount := range fromMap {
			if currentMax.Amount == nil || totalAmount.Cmp(currentMax.Amount) > 0 {
				currentMax = MaxTransferrer{
					Address: from,
					Amount:  totalAmount,
					Time:    time.Now(),
				}
			}
		}
		maxTransferByToken[token] = currentMax
	}

	return maxTransferByToken
}
