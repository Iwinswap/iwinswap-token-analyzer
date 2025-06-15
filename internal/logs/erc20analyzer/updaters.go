package erc20analyzer

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// MergeMaxTransferrers combines two maps of MaxTransferrer records.
// It returns a new map containing the merged result, leaving the original maps untouched.
//
// The merging logic is as follows:
//  1. All records from the 'old' map are carried over.
//  2. Records from the 'new' map are then merged. If a token exists in both maps,
//     the record with the larger transfer Amount is chosen.
func MergeMaxTransferrers(
	old, new map[common.Address]MaxTransferrer,
) map[common.Address]MaxTransferrer {

	// 1. Create a new map to store the result, pre-allocating its capacity
	//    and starting with a copy of the old records. This is efficient.
	merged := make(map[common.Address]MaxTransferrer, len(old))
	for token, transferrer := range old {
		merged[token] = transferrer
	}

	// 2. Iterate through the new records and merge them into our result map.
	for token, newTransferrer := range new {
		existingTransferrer, ok := merged[token]

		if !ok || newTransferrer.Amount.Cmp(existingTransferrer.Amount) > 0 {
			// If the token is not in our merged map yet, OR
			// if the new transferrer's amount is greater than the existing one,
			// then we update our merged map with the new record.
			merged[token] = newTransferrer
		}
	}

	return merged
}

// ExpireMaxTransferrers filters a map of MaxTransferrer records, returning a new
// map containing only the records that are not considered stale.
func ExpireMaxTransferrers(
	records map[common.Address]MaxTransferrer,
	staleAfter time.Duration,
) map[common.Address]MaxTransferrer {
	// Pre-allocate the new map with a reasonable starting capacity.
	freshRecords := make(map[common.Address]MaxTransferrer, len(records))

	for token, transferrer := range records {
		// Keep the record only if the time elapsed since it was recorded
		// is less than the stale duration.
		if time.Since(transferrer.Time) < staleAfter {
			freshRecords[token] = transferrer
		}
	}
	return freshRecords
}
