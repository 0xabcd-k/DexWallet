package chainreceipt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const EVMValue = "evm_value"
const EVMLog = "evm_log"
const EVMNativeProgram = "evm"
const ERC20TransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
const zeroEVMAddress = "0x0000000000000000000000000000000000000000"

type BaseRequest struct {
	Network   string `json:"network"`
	Hash      string `json:"hash"`
	LogIndex  *int   `json:"logIndex,omitempty"`
	AssetID   string `json:"assetId"`
	Recipient string `json:"recipient"`
	Atoms     string `json:"atoms"`
}
type BaseSource interface {
	Catalog() []chainassets.Network
	ReadBaseEvidence(context.Context, string, string) (chainassets.BaseEvidence, error)
}
type BaseVerifier struct{ source BaseSource }

func NewBaseVerifier(source BaseSource) *BaseVerifier { return &BaseVerifier{source} }

type BaseService struct {
	verifier *BaseVerifier
	store    *Service
}

func NewBase(conn sqlx.SqlConn, tx Transactor, source BaseSource) *BaseService {
	return &BaseService{NewBaseVerifier(source), &Service{conn: conn, tx: tx}}
}
func (s *BaseService) Record(ctx context.Context, r BaseRequest) (Stored, error) {
	v, err := s.verifier.Verify(ctx, r)
	if err != nil {
		return Stored{}, err
	}
	return s.store.record(ctx, v)
}

// Find recovers an already recorded event, without claiming an order or querying RPC.
func (s *BaseService) Find(ctx context.Context, network, hash, kind string, index int) (Receipt, error) {
	if (network != chainassets.BaseMainnet && network != chainassets.BaseSepolia) || (kind != EVMValue && kind != EVMLog) || index < 0 || (kind == EVMValue && index != 0) || !hexData(hash, 32) {
		return Receipt{}, ErrInvalidRequest
	}
	return s.store.findEvent(ctx, network, strings.ToLower(hash), kind, index)
}

func hexData(s string, size int) bool {
	if len(s) != 2+size*2 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, err := hex.DecodeString(s[2:])
	return err == nil
}
func quantity(s string) (*big.Int, error) {
	if len(s) < 3 || len(s) > 66 || !strings.HasPrefix(s, "0x") || (len(s) > 3 && s[2] == '0') {
		return nil, bad("invalid hex quantity")
	}
	for _, c := range s[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return nil, bad("invalid hex quantity")
		}
	}
	n, ok := new(big.Int).SetString(s[2:], 16)
	if !ok {
		return nil, bad("invalid hex quantity")
	}
	return n, nil
}
func decimal256(s string) bool {
	if len(s) == 0 || len(s) > 78 {
		return false
	}
	n, ok := new(big.Int).SetString(s, 10)
	return ok && n.Sign() > 0 && n.BitLen() <= 256 && n.String() == s
}
func normalizeHexFields(fields ...*string) {
	for _, f := range fields {
		*f = strings.ToLower(*f)
	}
}

func (v *BaseVerifier) Verify(ctx context.Context, r BaseRequest) (Receipt, error) {
	if !hexData(r.Hash, 32) || !hexData(r.Recipient, 20) || strings.EqualFold(r.Recipient, zeroEVMAddress) || !decimal256(r.Atoms) || (r.LogIndex != nil && (*r.LogIndex < 0 || *r.LogIndex > math.MaxInt32)) {
		return Receipt{}, ErrInvalidRequest
	}
	var asset chainassets.Asset
	for _, n := range v.source.Catalog() {
		if n.ID == r.Network && n.Family == chainassets.Base {
			for _, a := range n.Assets {
				if a.ID == r.AssetID {
					asset = a
					break
				}
			}
		}
	}
	if asset.ID == "" || (asset.Origin == chainassets.Native) != (r.LogIndex == nil) {
		return Receipt{}, ErrInvalidRequest
	}
	r.Hash = strings.ToLower(r.Hash)
	r.Recipient = strings.ToLower(r.Recipient)
	evidence, err := v.source.ReadBaseEvidence(ctx, r.Network, r.Hash)
	if err != nil {
		return Receipt{}, fmt.Errorf("read Base receipt %s/%s: %w", r.Network, r.Hash, err)
	}
	return verifyBase(r, asset, evidence)
}

type baseTransaction struct{ Hash, BlockHash, BlockNumber, TransactionIndex, ChainID, Type, From, To, Value, Input, Gas, Nonce string }
type baseLog struct {
	Address, BlockHash, BlockNumber, TransactionHash, TransactionIndex, LogIndex, Data string
	Topics                                                                             []string
	Removed                                                                            *bool
}
type baseReceipt struct {
	TransactionHash, BlockHash, BlockNumber, TransactionIndex, Type, Status, From, To, GasUsed, EffectiveGasPrice, L1Fee string
	Logs                                                                                                                 *[]baseLog
}
type baseBlock struct {
	Hash, Number string
	Transactions []string
}

func verifyBase(r BaseRequest, asset chainassets.Asset, e chainassets.BaseEvidence) (Receipt, error) {
	var out Receipt
	if null(e.Transaction) || null(e.Receipt) {
		return out, ErrUnconfirmed
	}
	var tx baseTransaction
	var receipt baseReceipt
	var head, block baseBlock
	if json.Unmarshal(e.Transaction, &tx) != nil || json.Unmarshal(e.Receipt, &receipt) != nil {
		return out, bad("invalid Base transaction/receipt")
	}
	if null(e.Finalized) || null(e.Block) {
		return out, ErrUnconfirmed
	}
	if json.Unmarshal(e.Finalized, &head) != nil || json.Unmarshal(e.Block, &block) != nil {
		return out, bad("missing canonical/finalized block")
	}
	normalizeHexFields(&tx.Hash, &tx.BlockHash, &tx.BlockNumber, &tx.TransactionIndex, &tx.ChainID, &tx.Type, &tx.From, &tx.To, &tx.Value, &tx.Input, &tx.Gas, &tx.Nonce)
	normalizeHexFields(&receipt.TransactionHash, &receipt.BlockHash, &receipt.BlockNumber, &receipt.TransactionIndex, &receipt.Type, &receipt.Status, &receipt.From, &receipt.To, &receipt.GasUsed, &receipt.EffectiveGasPrice, &receipt.L1Fee, &head.Hash, &head.Number, &block.Hash, &block.Number)
	chainID := "0x2105"
	if r.Network == chainassets.BaseSepolia {
		chainID = "0x14a34"
	}
	if tx.ChainID != chainID || tx.Hash != r.Hash || !hexData(tx.BlockHash, 32) || !hexData(tx.From, 20) || tx.From == zeroEVMAddress || !hexData(tx.To, 20) {
		return out, bad("wrong chain, transaction or accounts")
	}
	switch tx.Type {
	case "0x0", "0x1", "0x2", "0x4":
	default:
		return out, ErrUnsupported
	}
	if receipt.Type != tx.Type || receipt.TransactionHash != r.Hash || receipt.BlockHash != tx.BlockHash || receipt.BlockNumber != tx.BlockNumber || receipt.TransactionIndex != tx.TransactionIndex || receipt.From != tx.From || receipt.To != tx.To {
		return out, bad("transaction and receipt disagree")
	}
	height, err := quantity(tx.BlockNumber)
	if err != nil || !height.IsInt64() || height.Sign() <= 0 {
		return out, bad("invalid block height")
	}
	index, err := quantity(tx.TransactionIndex)
	if err != nil || !index.IsInt64() {
		return out, bad("invalid transaction index")
	}
	finalized, err := quantity(head.Number)
	if err != nil || !hexData(head.Hash, 32) {
		return out, bad("invalid finalized head")
	}
	if finalized.Cmp(height) < 0 {
		return out, ErrUnconfirmed
	}
	if block.Hash != tx.BlockHash || block.Number != tx.BlockNumber || index.Int64() >= int64(len(block.Transactions)) || !strings.EqualFold(block.Transactions[index.Int64()], r.Hash) || (finalized.Cmp(height) == 0 && head.Hash != block.Hash) {
		return out, bad("transaction absent from canonical finalized block")
	}
	if receipt.Status == "0x0" {
		return out, ErrExecutionFailed
	}
	if receipt.Status != "0x1" {
		return out, bad("invalid execution status")
	}
	value, err := quantity(tx.Value)
	if err != nil {
		return out, err
	}
	gasLimit, err := quantity(tx.Gas)
	if err != nil {
		return out, err
	}
	if _, err = quantity(tx.Nonce); err != nil {
		return out, err
	}
	if len(tx.Input) < 2 || len(tx.Input)%2 != 0 || !strings.HasPrefix(tx.Input, "0x") {
		return out, bad("invalid calldata")
	}
	if _, err = hex.DecodeString(tx.Input[2:]); err != nil {
		return out, bad("invalid calldata")
	}
	gas, err := quantity(receipt.GasUsed)
	if err != nil || gas.Sign() <= 0 || gas.Cmp(gasLimit) > 0 {
		return out, bad("invalid gas used")
	}
	price, err := quantity(receipt.EffectiveGasPrice)
	if err != nil {
		return out, err
	}
	l1, err := quantity(receipt.L1Fee)
	if err != nil {
		return out, bad("missing/invalid L1 fee")
	}
	var operatorRaw string
	if json.Unmarshal(e.OperatorFee, &operatorRaw) != nil || !hexData(operatorRaw, 32) {
		return out, bad("missing historical operator fee")
	}
	operator, _ := new(big.Int).SetString(operatorRaw[2:], 16)
	execution := new(big.Int).Mul(gas, price)
	total := new(big.Int).Add(execution, l1)
	total.Add(total, operator)
	if total.BitLen() > 256 {
		return out, bad("network fee exceeds uint256")
	}
	if receipt.Logs == nil {
		return out, bad("missing receipt logs")
	}
	seen := map[int]bool{}
	var selected *baseLog
	for i := range *receipt.Logs {
		l := &(*receipt.Logs)[i]
		normalizeHexFields(&l.Address, &l.BlockHash, &l.BlockNumber, &l.TransactionHash, &l.TransactionIndex, &l.LogIndex, &l.Data)
		for j := range l.Topics {
			l.Topics[j] = strings.ToLower(l.Topics[j])
		}
		pos, err := quantity(l.LogIndex)
		if err != nil || !pos.IsInt64() || pos.Int64() > math.MaxInt32 {
			return out, bad("invalid log position")
		}
		at := int(pos.Int64())
		if seen[at] || l.Removed == nil || *l.Removed || l.BlockHash != tx.BlockHash || l.BlockNumber != tx.BlockNumber || l.TransactionHash != tx.Hash || l.TransactionIndex != tx.TransactionIndex || !hexData(l.Address, 20) {
			return out, bad("inconsistent, duplicate or removed log")
		}
		seen[at] = true
		if r.LogIndex != nil && at == *r.LogIndex {
			selected = l
		}
	}
	event := Event{AssetID: asset.ID, Decimals: asset.Decimals}
	if asset.Origin == chainassets.Native {
		if tx.Input != "0x" || value.Sign() == 0 {
			return out, ErrUnsupported
		}
		event.Kind = EVMValue
		event.ProgramID = EVMNativeProgram
		event.SourceAccount = tx.From
		event.RecipientAccount = tx.To
		event.Atoms = value.String()
	} else {
		if selected == nil {
			return out, ErrUnsupported
		}
		l := selected
		if l.Address != strings.ToLower(asset.Contract) || len(l.Topics) != 3 || l.Topics[0] != ERC20TransferTopic || !hexData(l.Topics[1], 32) || !hexData(l.Topics[2], 32) || !hexData(l.Data, 32) || l.Topics[1][2:26] != strings.Repeat("0", 24) || l.Topics[2][2:26] != strings.Repeat("0", 24) {
			return out, bad("not an exact supported token Transfer")
		}
		amount, _ := new(big.Int).SetString(l.Data[2:], 16)
		event.Kind = EVMLog
		event.Instruction = *r.LogIndex
		event.Mint = l.Address
		event.ProgramID = l.Address
		event.SourceAccount = "0x" + l.Topics[1][26:]
		event.RecipientAccount = "0x" + l.Topics[2][26:]
		event.Atoms = amount.String()
	}
	event.SourceOwner = event.SourceAccount
	event.RecipientOwner = event.RecipientAccount
	if event.SourceOwner == zeroEVMAddress || event.SourceOwner == event.RecipientOwner || event.RecipientOwner != r.Recipient || event.Atoms != r.Atoms {
		return out, bad("amount/recipient differs or mint/self transfer")
	}
	// Exclude the moving finalized head; retain transaction, receipt and exact
	// historical fee facts. These typed structures contain no floating values.
	normalized, _ := json.Marshal(struct {
		Transaction baseTransaction
		Receipt     baseReceipt
		Operator    string
	}{tx, receipt, operator.String()})
	digest := sha256.Sum256(normalized)
	out = Receipt{Transaction: Transaction{Network: r.Network, Signature: r.Hash, Slot: height.Int64(), FeePayer: tx.From, FeeAtoms: total.String(), Fingerprint: fmt.Sprintf("%x", digest), BlockHash: tx.BlockHash, ExecutionFeeAtoms: execution.String(), L1FeeAtoms: l1.String(), OperatorFeeAtoms: operator.String()}, Event: event, evidence: e}
	return out, nil
}
