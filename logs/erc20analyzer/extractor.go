package erc20analyzer

import (
	"math/big"
	"time"

	"github.com/Iwinswap/iwinswap-token-analyzer/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type MaxTransferrer struct {
	Address common.Address
	Amount  *big.Int
	Time    time.Time
}

// ExtractMaxSingleTransfer finds the largest single transfer from an allowed address.
func ExtractMaxSingleTransfer(logs []types.Log, isAllowedAddress func(common.Address) bool) map[common.Address]MaxTransferrer {
	maxTransferByToken := make(map[common.Address]MaxTransferrer)

	for _, l := range logs {
		from, _, value, err := abi.ParseERC20TransferEvent(l)
		if err != nil {
			continue
		}

		// 💡 Only process transfers where the sender is allowed.
		if !isAllowedAddress(from) {
			continue
		}

		tokenAddress := l.Address
		currentMax, ok := maxTransferByToken[tokenAddress]

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

// ExtractMaxTotalVolumeTransferrer finds the allowed address with the highest total volume.
func ExtractMaxTotalVolumeTransferrer(logs []types.Log, isAllowedAddress func(common.Address) bool) map[common.Address]MaxTransferrer {
	totals := make(map[common.Address]map[common.Address]*big.Int)

	for _, l := range logs {
		from, _, value, err := abi.ParseERC20TransferEvent(l)
		if err != nil {
			continue
		}

		// 💡 Only aggregate transfers where the sender is allowed.
		if !isAllowedAddress(from) {
			continue
		}

		tokenAddress := l.Address
		if _, ok := totals[tokenAddress]; !ok {
			totals[tokenAddress] = make(map[common.Address]*big.Int)
		}
		if _, ok := totals[tokenAddress][from]; !ok {
			totals[tokenAddress][from] = new(big.Int)
		}
		totals[tokenAddress][from].Add(totals[tokenAddress][from], value)
	}

	maxTransferByToken := make(map[common.Address]MaxTransferrer)
	for token, fromMap := range totals {
		var currentMax MaxTransferrer
		isFirst := true
		for from, totalAmount := range fromMap {
			if isFirst || totalAmount.Cmp(currentMax.Amount) > 0 {
				currentMax = MaxTransferrer{
					Address: from,
					Amount:  totalAmount,
					Time:    time.Now(),
				}
				isFirst = false
			}
		}
		if !isFirst {
			maxTransferByToken[token] = currentMax
		}
	}
	return maxTransferByToken
}
