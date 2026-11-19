package chainreceipt

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

const maxEVMCallDepth = 1024

// Index is a preorder position in the COMPLETE callTracer tree, including root
// zero and unsuccessful/non-value frames. Filtering first would change identity.
type baseCall struct {
	Type, From, To, Value, Input, Output, Gas, GasUsed, Error string
	Calls                                                     []baseCall
}

func verifyBaseCall(index int, asset chainassets.Asset, tx baseTransaction, receipt baseReceipt, raw json.RawMessage) (Event, string, error) {
	var root baseCall
	if null(raw) || json.Unmarshal(raw, &root) != nil {
		return Event{}, "", bad("missing or malformed full call trace")
	}
	var selected *baseCall
	selectedFailed := false
	position := 0
	var walk func(*baseCall, string, bool, bool, int) error
	walk = func(c *baseCall, parentAccount string, failed, static bool, depth int) error {
		if depth > maxEVMCallDepth {
			return bad("call depth exceeds EVM limit")
		}
		normalizeHexFields(&c.From, &c.To, &c.Value, &c.Input, &c.Output, &c.Gas, &c.GasUsed)
		c.Type = strings.ToUpper(c.Type)
		if !hexData(c.From, 20) || (depth > 0 && parentAccount != "" && c.From != parentAccount) {
			return bad("call sender differs from execution context")
		}
		failed = failed || c.Error != ""
		// A reverted CREATE may omit its target, so its already-failed subtree
		// has no recoverable parent address; it can never become a success event.
		account := c.To
		switch c.Type {
		case "CALL", "CREATE", "CREATE2", "SELFDESTRUCT":
		case "STATICCALL":
			static = true
			if c.Value == "" {
				c.Value = "0x0"
			}
		case "DELEGATECALL", "CALLCODE":
			account = parentAccount
		default:
			return ErrUnsupported
		}
		if !hexData(c.To, 20) && !((c.Type == "CREATE" || c.Type == "CREATE2") && c.Error != "" && c.To == "") {
			return bad("missing call target")
		}
		value, err := quantity(c.Value)
		if err != nil {
			return err
		}
		if c.Type == "STATICCALL" && value.Sign() != 0 {
			return bad("value in STATICCALL")
		}
		if c.Output == "" {
			c.Output = "0x"
		}
		if !hexData(c.Input, (len(c.Input)-2)/2) || !hexData(c.Output, (len(c.Output)-2)/2) {
			return bad("malformed call data")
		}
		gas, err := quantity(c.Gas)
		if err != nil || !gas.IsUint64() {
			return bad("invalid call gas")
		}
		used, err := quantity(c.GasUsed)
		if err != nil || !used.IsUint64() || used.Cmp(gas) > 0 {
			return bad("invalid call gas used")
		}
		if position == index {
			selected, selectedFailed = c, failed
			if static {
				return bad("value transfer under STATICCALL")
			}
		}
		position++
		for i := range c.Calls {
			if err := walk(&c.Calls[i], account, failed, static, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(&root, "", false, false, 0); err != nil {
		return Event{}, "", err
	}
	if root.Type != "CALL" || root.From != tx.From || root.To != tx.To || root.Value != tx.Value || root.Input != tx.Input || root.Gas != tx.Gas || root.GasUsed != receipt.GasUsed || root.Error != "" {
		return Event{}, "", bad("call trace root disagrees with transaction/receipt")
	}
	if selected == nil {
		return Event{}, "", bad("call position absent from full trace")
	}
	if selectedFailed {
		return Event{}, "", ErrExecutionFailed
	}
	if selected.Type != "CALL" {
		return Event{}, "", ErrUnsupported
	}
	value, _ := quantity(selected.Value)
	if value.Sign() <= 0 {
		return Event{}, "", bad("call has no ETH value")
	}
	canonical, _ := json.Marshal(root)
	digest := sha256.Sum256(canonical)
	return Event{Kind: EVMCall, Instruction: index, AssetID: asset.ID, Decimals: asset.Decimals, ProgramID: EVMNativeProgram, SourceAccount: selected.From, RecipientAccount: selected.To, Atoms: value.String()}, fmt.Sprintf("%x", digest), nil
}
