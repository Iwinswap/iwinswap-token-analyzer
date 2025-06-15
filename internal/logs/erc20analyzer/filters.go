package erc20analyzer

import (
	"github.com/Iwinswap/iwinswap-token-analyzer/internal/abi"
	"github.com/ethereum/go-ethereum/core/types"
)

var ERC20TransferTopic = abi.ERC20ABI.Events["Transfer"].ID

func FilterERC20TransferLogs(
	logs []types.Log,
) (filtered []types.Log) {

	for _, l := range logs {
		if len(l.Topics) > 0 && l.Topics[0] == ERC20TransferTopic {
			filtered = append(filtered, l)
		}
	}

	return filtered
}
