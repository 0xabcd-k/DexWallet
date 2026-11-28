package chainreceipt

import (
	"encoding/json"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseeth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Preserve callTracer preorder indices, including reverted subtrees. A deposit
// permits one successful movement of ETH to the platform with no onward execution.
// With no match, verifyBase still checks finality/revert before rejecting the index.
func nativeDepositCallIndex(raw json.RawMessage, grant baseeth.Redemption) (int, error) {
	if nullOrAbsent(raw) {
		return -1, nil
	}
	var root baseCall
	if err := json.Unmarshal(raw, &root); err != nil {
		return -1, bad("malformed ETH deposit trace")
	}
	next, found, movements := 0, -1, 0
	var walk func(*baseCall, bool, int) error
	walk = func(c *baseCall, failed bool, depth int) error {
		if depth > maxEVMCallDepth {
			return bad("ETH deposit trace exceeds EVM depth")
		}
		pos := next
		next++
		failed = failed || c.Error != "" || strings.EqualFold(c.Type, "STATICCALL")
		if !failed {
			switch strings.ToUpper(c.Type) {
			case "CALL", "CREATE", "CREATE2", "SELFDESTRUCT":
				n, err := quantity(strings.ToLower(c.Value))
				if err != nil {
					return err
				}
				if n.Sign() > 0 {
					movements++
					if pos > 0 && strings.EqualFold(c.Type, "CALL") && c.Input == "0x" && len(c.Calls) == 0 && n.Cmp(grant.Amount) == 0 && strings.EqualFold(c.From, grant.Delegator.Hex()) && strings.EqualFold(c.To, grant.Delegate.Hex()) {
						found = pos
					}
				}
			}
		}
		for i := range c.Calls {
			if err := walk(&c.Calls[i], failed, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(&root, false, 0); err != nil {
		return -1, err
	}
	if movements != 1 {
		return -1, nil
	}
	return found, nil
}

func verifyRedemptionEvent(receipt baseReceipt, redemption baseeth.Redemption, signature string) error {
	grant, err := redemption.Signed(signature)
	if err != nil {
		return err
	}
	data, err := grant.EventData()
	if err != nil {
		return err
	}
	owner := common.BytesToHash(grant.Delegator.Bytes()).Hex()
	payer := common.BytesToHash(grant.Delegate.Bytes()).Hex()
	seen := false
	for _, log := range *receipt.Logs {
		if !strings.EqualFold(log.Address, baseeth.Manager) || len(log.Topics) == 0 || !strings.EqualFold(log.Topics[0], baseeth.RedeemedTopic.Hex()) {
			continue
		}
		if seen || len(log.Topics) != 3 || !strings.EqualFold(log.Topics[1], owner) || !strings.EqualFold(log.Topics[2], payer) || !strings.EqualFold(log.Data, hexutil.Encode(data)) {
			return bad("ETH redemption event differs from signed order")
		}
		seen = true
	}
	if !seen {
		return bad("missing ETH redemption event")
	}
	return nil
}
