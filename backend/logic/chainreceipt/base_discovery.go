package chainreceipt

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
)

type baseHistoryBlock struct {
	baseBlock
	ParentHash, Timestamp string
	height, at            int64
}

func historyBlock(raw json.RawMessage) (baseHistoryBlock, error) {
	var b baseHistoryBlock
	if json.Unmarshal(raw, &b) != nil || !hexData(b.Hash, 32) || !hexData(b.ParentHash, 32) || b.Transactions == nil {
		return b, bad("missing Base history block")
	}
	n, err := quantity(b.Number)
	if err != nil || !n.IsInt64() || n.Sign() <= 0 {
		return b, bad("invalid Base history height")
	}
	t, err := quantity(b.Timestamp)
	if err != nil || !t.IsInt64() || t.Sign() <= 0 {
		return b, bad("invalid Base history timestamp")
	}
	b.height, b.at = n.Int64(), t.Int64()
	normalizeHexFields(&b.Hash, &b.ParentHash)
	seen := make(map[string]bool, len(b.Transactions))
	for i := range b.Transactions {
		hash := strings.ToLower(b.Transactions[i])
		if !hexData(hash, 32) || seen[hash] {
			return b, bad("invalid/duplicate transaction in Base block")
		}
		seen[hash], b.Transactions[i] = true, hash
	}
	return b, nil
}

type baseIncoming struct {
	hash, kind, owner, atoms string
	index                    int
}

// A successful contract creation has an explicit null destination. A missing
// destination on an ordinary receipt cannot safely be classified as creation.
type historyReceipt struct {
	baseReceipt
	Destination json.RawMessage `json:"to"`
}

type blockTrace struct {
	TxHash, Error string
	Result        *baseCall
}

func baseIncomingEvents(b baseHistoryBlock, asset string, receiptsRaw, callsRaw json.RawMessage) ([]baseIncoming, error) {
	var receipts []historyReceipt
	if json.Unmarshal(receiptsRaw, &receipts) != nil || receipts == nil || len(receipts) != len(b.Transactions) {
		return nil, bad("incomplete Base block receipts")
	}
	native := asset != baseusdc.AssetID
	var traces []blockTrace
	if native && (json.Unmarshal(callsRaw, &traces) != nil || traces == nil || len(traces) != len(receipts)) {
		return nil, bad("incomplete Base block call traces")
	}
	var events []baseIncoming
	seenLogs := make(map[int]bool)
	for i, r := range receipts {
		hash := b.Transactions[i]
		index, err := quantity(r.TransactionIndex)
		if err != nil || !index.IsInt64() || index.Int64() != int64(i) || !strings.EqualFold(r.TransactionHash, hash) || !strings.EqualFold(r.BlockHash, b.Hash) || r.BlockNumber != b.Number || (r.Status != "0x1" && r.Status != "0x0") {
			return nil, bad("receipt differs from scanned Base block")
		}
		if native && !strings.EqualFold(traces[i].TxHash, hash) {
			return nil, bad("trace transaction differs from scanned Base block")
		}
		if _, err = quantity(r.Type); err != nil {
			return nil, bad("missing Base transaction type")
		}
		if r.Status == "0x0" || !supportedBaseType(r.Type) {
			continue
		}
		if string(r.Destination) == "null" {
			continue
		}
		if json.Unmarshal(r.Destination, &r.To) != nil || !hexData(r.To, 20) || !hexData(r.From, 20) {
			return nil, bad("missing Base receipt accounts")
		}
		if native {
			tr := traces[i]
			if tr.Error != "" || tr.Result == nil || tr.Result.Type != "CALL" || tr.Result.Error != "" || !strings.EqualFold(tr.Result.From, r.From) || !strings.EqualFold(tr.Result.To, r.To) || tr.Result.Input == "" {
				return nil, bad("missing/inconsistent Base call trace root")
			}
			pos := 0
			if err = discoverBaseCalls(tr.Result, hash, &pos, false, 0, &events); err != nil {
				return nil, err
			}
			continue
		}
		if r.Logs == nil {
			return nil, bad("missing Base receipt logs")
		}
		for _, l := range *r.Logs {
			if !hexData(l.Address, 20) {
				return nil, bad("invalid Base log address")
			}
			if !strings.EqualFold(l.Address, baseusdc.SepoliaContract) {
				continue
			}
			if l.Topics == nil {
				return nil, bad("missing Base USDC log topics")
			}
			if len(l.Topics) == 0 || !strings.EqualFold(l.Topics[0], ERC20TransferTopic) {
				continue
			}
			n, e := quantity(l.LogIndex)
			if e != nil || !n.IsInt64() || n.Int64() > math.MaxInt32 || seenLogs[int(n.Int64())] || l.Removed == nil || *l.Removed || !strings.EqualFold(l.BlockHash, b.Hash) || l.BlockNumber != b.Number || !strings.EqualFold(l.TransactionHash, hash) || l.TransactionIndex != r.TransactionIndex {
				return nil, bad("invalid or duplicate Base Transfer log position")
			}
			pos := int(n.Int64())
			seenLogs[pos] = true
			from, to, amount, e := decodeBaseTransferLog(l, baseusdc.SepoliaContract)
			if e != nil {
				return nil, e
			}
			if from != zeroEVMAddress && from != to && amount != "0" {
				events = append(events, baseIncoming{hash, EVMLog, to, amount, pos})
			}
		}
	}
	return events, nil
}

// Index every frame, including failed and static subtrees, to retain the same
// preorder positions as the independent transaction-level verifier.
func discoverBaseCalls(c *baseCall, hash string, position *int, blocked bool, depth int, events *[]baseIncoming) error {
	if depth > maxEVMCallDepth {
		return bad("Base block trace exceeds EVM depth")
	}
	i := *position
	*position = i + 1
	kind := strings.ToUpper(c.Type)
	switch kind {
	case "CALL", "CREATE", "CREATE2", "DELEGATECALL", "CALLCODE", "SELFDESTRUCT", "STATICCALL":
	default:
		return bad("unknown Base block call type")
	}
	blocked = blocked || c.Error != "" || kind == "STATICCALL"
	if !blocked && kind == "CALL" {
		amount, err := quantity(c.Value)
		if err != nil || !hexData(c.From, 20) || !hexData(c.To, 20) {
			return bad("invalid Base value call")
		}
		if amount.Sign() > 0 && !strings.EqualFold(c.From, c.To) && !strings.EqualFold(c.From, zeroEVMAddress) {
			event := EVMCall
			if i == 0 {
				event = EVMValue
			}
			if i > 0 || c.Input == "0x" {
				*events = append(*events, baseIncoming{hash, event, strings.ToLower(c.To), amount.String(), i})
			}
		}
	}
	for j := range c.Calls {
		if err := discoverBaseCalls(&c.Calls[j], hash, position, blocked, depth+1, events); err != nil {
			return err
		}
	}
	return nil
}
