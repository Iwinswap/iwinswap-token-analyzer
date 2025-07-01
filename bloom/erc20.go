package bloom

import (
	"github.com/Iwinswap/iwinswap-token-analyzer/abi"
	"github.com/ethereum/go-ethereum/core/types"
)

var ERC20TransferTopic = abi.ERC20ABI.Events["Transfer"].ID

func ERC20TransferLikelyInBloom(
	bloom types.Bloom,
) bool {
	return bloom.Test(ERC20TransferTopic.Bytes())
}
