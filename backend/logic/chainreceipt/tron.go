package chainreceipt

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/mr-tron/base58"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const TRONTransfer = "tron_transfer"
const TRONLog = "tron_log"
const tronTransferContract = "TransferContract"
const tronTriggerContract = "TriggerSmartContract"
const tronAddressPrefix byte = 0x41

type TRONRequest struct {
	Network   string `json:"network"`
	Hash      string `json:"hash"`
	LogIndex  *int   `json:"logIndex,omitempty"`
	AssetID   string `json:"assetId"`
	Recipient string `json:"recipient"`
	Atoms     string `json:"atoms"`
}
type TRONSource interface {
	Catalog() []chainassets.Network
	ReadTRONEvidence(context.Context, string, string) (chainassets.TRONEvidence, error)
}
type TRONVerifier struct{ source TRONSource }

func NewTRONVerifier(source TRONSource) *TRONVerifier { return &TRONVerifier{source} }

type TRONService struct {
	verifier *TRONVerifier
	store    *Service
}

func NewTRON(conn sqlx.SqlConn, tx Transactor, source TRONSource) *TRONService {
	return &TRONService{NewTRONVerifier(source), &Service{conn: conn, tx: tx}}
}
func (s *TRONService) Record(ctx context.Context, r TRONRequest) (Stored, error) {
	v, err := s.verifier.Verify(ctx, r)
	if err != nil {
		return Stored{}, err
	}
	return s.store.record(ctx, v)
}
func (s *TRONService) Find(ctx context.Context, network, hash, kind string, index int) (Receipt, error) {
	if (network != chainassets.TRONMainnet && network != chainassets.TRONNile) || !tronHex(hash, 32) || (kind != TRONTransfer && kind != TRONLog) || index < 0 || index > math.MaxInt32 || (kind == TRONTransfer && index != 0) {
		return Receipt{}, ErrInvalidRequest
	}
	return s.store.findEvent(ctx, network, strings.ToLower(hash), kind, index)
}
func tronHex(s string, bytes int) bool { return hexData("0x"+s, bytes) }
func tronAddress(raw string) (string, error) {
	if !tronHex(raw, 21) || !strings.HasPrefix(raw, "41") {
		return "", bad("invalid TRON hex address")
	}
	data, _ := hex.DecodeString(raw)
	first := sha256.Sum256(data)
	second := sha256.Sum256(first[:])
	return base58.Encode(append(data, second[:4]...)), nil
}
func tronUserAddress(address string) bool {
	raw, err := base58.Decode(address)
	if err != nil || len(raw) != 25 || raw[0] != tronAddressPrefix {
		return false
	}
	canonical, err := tronAddress(hex.EncodeToString(raw[:21]))
	return err == nil && canonical == address && hex.EncodeToString(raw[1:21]) != strings.Repeat("0", 40)
}
func (v *TRONVerifier) Verify(ctx context.Context, r TRONRequest) (Receipt, error) {
	if !tronHex(r.Hash, 32) || !tronUserAddress(r.Recipient) || !decimal256(r.Atoms) || (r.LogIndex != nil && (*r.LogIndex < 0 || *r.LogIndex > math.MaxInt32)) {
		return Receipt{}, ErrInvalidRequest
	}
	var asset chainassets.Asset
	for _, n := range v.source.Catalog() {
		if n.ID == r.Network && n.Family == chainassets.TRON {
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
	if asset.Origin == chainassets.Native {
		if _, err := strconv.ParseInt(r.Atoms, 10, 64); err != nil {
			return Receipt{}, ErrInvalidRequest
		}
	}
	r.Hash = strings.ToLower(r.Hash)
	evidence, err := v.source.ReadTRONEvidence(ctx, r.Network, r.Hash)
	if err != nil {
		return Receipt{}, fmt.Errorf("read TRON receipt %s/%s: %w", r.Network, r.Hash, err)
	}
	return verifyTRON(r, asset, evidence)
}

// Proto3 omits zero counters. Explicit null, fractional, negative, quoted or
// overflowing values are malformed evidence, not an omitted zero.
type tronCounter int64

func (n *tronCounter) UnmarshalJSON(b []byte) error {
	value, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil || value < 0 {
		return bad("invalid TRON integer counter")
	}
	*n = tronCounter(value)
	return nil
}

type tronContract struct {
	Type      string `json:"type"`
	Parameter struct {
		TypeURL string `json:"type_url"`
		Value   struct {
			Owner          string      `json:"owner_address"`
			To             string      `json:"to_address"`
			Amount         tronCounter `json:"amount"`
			Contract       string      `json:"contract_address"`
			Data           string      `json:"data"`
			CallValue      tronCounter `json:"call_value"`
			CallTokenValue tronCounter `json:"call_token_value"`
			TokenID        tronCounter `json:"token_id"`
		} `json:"value"`
	} `json:"parameter"`
}
type tronTransaction struct {
	ID     string `json:"txID"`
	RawHex string `json:"raw_data_hex"`
	Raw    struct {
		Contracts []tronContract `json:"contract"`
	} `json:"raw_data"`
	Ret []struct {
		ContractRet string      `json:"contractRet"`
		Ret         string      `json:"ret"`
		Fee         tronCounter `json:"fee"`
	} `json:"ret"`
}
type tronResourceReceipt struct {
	EnergyUsage        tronCounter `json:"energy_usage"`
	EnergyFee          tronCounter `json:"energy_fee"`
	OriginEnergyUsage  tronCounter `json:"origin_energy_usage"`
	EnergyUsageTotal   tronCounter `json:"energy_usage_total"`
	NetUsage           tronCounter `json:"net_usage"`
	NetFee             tronCounter `json:"net_fee"`
	EnergyPenaltyTotal tronCounter `json:"energy_penalty_total"`
	Result             string      `json:"result"`
}
type tronEventLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}
type tronInfo struct {
	ID        string               `json:"id"`
	Fee       tronCounter          `json:"fee"`
	Height    tronCounter          `json:"blockNumber"`
	Timestamp tronCounter          `json:"blockTimeStamp"`
	Result    string               `json:"result"`
	Receipt   *tronResourceReceipt `json:"receipt"`
	Logs      []tronEventLog       `json:"log"`
}
type tronBlock struct {
	ID     string `json:"blockID"`
	Header *struct {
		Raw *struct {
			Number    tronCounter `json:"number"`
			Timestamp tronCounter `json:"timestamp"`
		} `json:"raw_data"`
	} `json:"block_header"`
	Transactions []struct {
		ID     string `json:"txID"`
		RawHex string `json:"raw_data_hex"`
	} `json:"transactions"`
}

func (b tronBlock) valid() bool {
	if !tronHex(b.ID, 32) || b.Header == nil || b.Header.Raw == nil {
		return false
	}
	raw, _ := hex.DecodeString(b.ID)
	return binary.BigEndian.Uint64(raw[:8]) == uint64(b.Header.Raw.Number)
}
func tronEmptyObject(raw json.RawMessage) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil && len(object) == 0
}

// Direct platform payouts bind the signed call as well as its single emitted Transfer.
func payoutTransferIndex(r TRONRequest, token string, c tronContract, logs []tronEventLog) (int, error) {
	v := c.Parameter.Value
	destination, err := tronAddress(v.Contract)
	data, dataErr := tronnative.TransferData(r.Recipient, r.Atoms)
	if err != nil || destination != token || dataErr != nil || !strings.EqualFold(v.Data, hex.EncodeToString(data)) || v.CallValue != 0 || v.CallTokenValue != 0 || v.TokenID != 0 {
		return 0, bad("TRC-20 payout call differs from authorization")
	}
	index := -1
	for i, log := range logs {
		emitter, err := tronAddress("41" + log.Address)
		if err != nil || emitter != token || len(log.Topics) == 0 || !strings.EqualFold(log.Topics[0], ERC20TransferTopic[2:]) {
			continue
		}
		if index >= 0 || i > math.MaxInt32 {
			return 0, bad("ambiguous TRC-20 payout Transfer")
		}
		index = i
	}
	if index < 0 {
		return 0, bad("missing authorized TRC-20 payout Transfer")
	}
	return index, nil
}

func verifyTRON(r TRONRequest, asset chainassets.Asset, e chainassets.TRONEvidence) (Receipt, error) {
	var out Receipt
	var genesis, head, block tronBlock
	if json.Unmarshal(e.Genesis, &genesis) != nil || !genesis.valid() {
		return out, bad("missing TRON genesis")
	}
	wantGenesis := chainassets.TRONMainnetGenesis
	if r.Network == chainassets.TRONNile {
		wantGenesis = chainassets.TRONNileGenesis
	}
	if genesis.ID != wantGenesis {
		return out, bad("wrong TRON network")
	}
	if tronEmptyObject(e.Transaction) || tronEmptyObject(e.Info) {
		return out, ErrUnconfirmed
	}
	var tx tronTransaction
	var info tronInfo
	if json.Unmarshal(e.Transaction, &tx) != nil || json.Unmarshal(e.Info, &info) != nil || tx.ID != r.Hash || info.ID != r.Hash || len(tx.Raw.Contracts) != 1 || len(tx.Ret) != 1 || info.Receipt == nil || info.Height <= 0 || info.Timestamp <= 0 {
		return out, bad("invalid TRON transaction/receipt")
	}
	raw, err := hex.DecodeString(tx.RawHex)
	if err != nil || len(raw) == 0 {
		return out, bad("invalid TRON raw transaction")
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != r.Hash {
		return out, bad("TRON txID does not hash raw transaction")
	}
	if tronEmptyObject(e.SolidHead) || tronEmptyObject(e.Block) {
		return out, ErrUnconfirmed
	}
	if json.Unmarshal(e.SolidHead, &head) != nil || json.Unmarshal(e.Block, &block) != nil || !head.valid() || !block.valid() {
		return out, bad("invalid TRON solid block")
	}
	if head.Header.Raw.Number < info.Height {
		return out, ErrUnconfirmed
	}
	if block.Header.Raw.Number != info.Height || block.Header.Raw.Timestamp != info.Timestamp || (head.Header.Raw.Number == info.Height && head.ID != block.ID) {
		return out, bad("TRON inclusion block differs")
	}
	matches := 0
	for _, member := range block.Transactions {
		if member.ID == r.Hash {
			matches++
			if member.RawHex != tx.RawHex {
				return out, bad("TRON block transaction differs")
			}
		}
	}
	if matches != 1 {
		return out, bad("TRON transaction absent or duplicated in solid block")
	}
	contract := tx.Raw.Contracts[0]
	if contract.Type != tronTransferContract && contract.Type != tronTriggerContract {
		return out, ErrUnsupported
	}
	if contract.Parameter.TypeURL != "type.googleapis.com/protocol."+contract.Type {
		return out, bad("TRON contract type differs")
	}
	resource := info.Receipt
	if tx.Ret[0].ContractRet != "SUCCESS" || tx.Ret[0].Ret == "FAILED" || info.Result == "FAILED" || (resource.Result != "" && resource.Result != "DEFAULT" && resource.Result != "SUCCESS") {
		switch tx.Ret[0].ContractRet {
		case "REVERT", "BAD_JUMP_DESTINATION", "OUT_OF_MEMORY", "PRECOMPILED_CONTRACT", "STACK_TOO_SMALL", "STACK_TOO_LARGE", "ILLEGAL_OPERATION", "STACK_OVERFLOW", "OUT_OF_ENERGY", "OUT_OF_TIME", "JVM_STACK_OVER_FLOW", "UNKNOWN", "TRANSFER_FAILED", "INVALID_CODE":
			return out, ErrExecutionFailed
		default:
			return out, bad("inconsistent TRON execution status")
		}
	}
	if (info.Result != "" && info.Result != "SUCESS") || (tx.Ret[0].Ret != "" && tx.Ret[0].Ret != "SUCESS") || (contract.Type == tronTriggerContract && resource.Result != "SUCCESS") {
		return out, bad("missing or invalid TRON execution status")
	}
	// Subtraction avoids overflow while allowing other documented TRX charges,
	// such as activation, memo and multisignature fees, in the total.
	if resource.NetFee > info.Fee || resource.EnergyFee > info.Fee-resource.NetFee || resource.EnergyUsage > resource.EnergyUsageTotal || resource.OriginEnergyUsage > resource.EnergyUsageTotal-resource.EnergyUsage || resource.EnergyPenaltyTotal > resource.EnergyUsageTotal {
		return out, bad("inconsistent TRON fees/resources")
	}
	owner, err := tronAddress(contract.Parameter.Value.Owner)
	if err != nil || !tronUserAddress(owner) {
		return out, bad("invalid TRON transaction owner")
	}
	event := Event{AssetID: asset.ID, Decimals: asset.Decimals}
	if asset.Origin == chainassets.Native {
		if contract.Type != tronTransferContract {
			return out, ErrUnsupported
		}
		recipient, err := tronAddress(contract.Parameter.Value.To)
		if err != nil || contract.Parameter.Value.Amount <= 0 {
			return out, bad("invalid TRX transfer")
		}
		event.Kind = TRONTransfer
		event.ProgramID = tronTransferContract
		event.SourceAccount = owner
		event.RecipientAccount = recipient
		event.Atoms = strconv.FormatInt(int64(contract.Parameter.Value.Amount), 10)
	} else {
		if contract.Type != tronTriggerContract {
			return out, ErrUnsupported
		}
		if _, err := tronAddress(contract.Parameter.Value.Contract); err != nil {
			return out, err
		}
		if !tronHex(contract.Parameter.Value.Data, len(contract.Parameter.Value.Data)/2) || len(contract.Parameter.Value.Data) < 8 {
			return out, bad("invalid TRON contract calldata")
		}
		index := 0
		if r.LogIndex == nil {
			index, err = payoutTransferIndex(r, asset.Contract, contract, info.Logs)
			if err != nil {
				return out, err
			}
		} else {
			index = *r.LogIndex
		}
		if index >= len(info.Logs) {
			return out, ErrUnsupported
		}
		log := info.Logs[index]
		if !tronHex(log.Address, 20) || len(log.Topics) != 3 || !strings.EqualFold(log.Topics[0], ERC20TransferTopic[2:]) || !tronHex(log.Topics[1], 32) || !tronHex(log.Topics[2], 32) || log.Topics[1][:24] != strings.Repeat("0", 24) || log.Topics[2][:24] != strings.Repeat("0", 24) || !tronHex(log.Data, 32) {
			return out, bad("not an exact TRC-20 Transfer")
		}
		emitter, _ := tronAddress("41" + log.Address)
		if emitter != asset.Contract {
			return out, bad("TRC-20 Transfer from wrong contract")
		}
		event.Kind = TRONLog
		event.Instruction = index
		event.ProgramID = emitter
		event.Mint = emitter
		event.SourceAccount, _ = tronAddress("41" + log.Topics[1][24:])
		event.RecipientAccount, _ = tronAddress("41" + log.Topics[2][24:])
		amount, _ := new(big.Int).SetString(log.Data, 16)
		event.Atoms = amount.String()
	}
	event.SourceOwner = event.SourceAccount
	event.RecipientOwner = event.RecipientAccount
	if !tronUserAddress(event.SourceOwner) || event.SourceOwner == event.RecipientOwner || event.RecipientOwner != r.Recipient || event.Atoms != r.Atoms {
		return out, bad("amount/recipient differs or mint/self transfer")
	}
	// The solid head advances. Only immutable transaction, inclusion and receipt
	// facts enter the fingerprint; all logs retain their original array positions.
	normalized, _ := json.Marshal(struct {
		Transaction tronTransaction
		Info        tronInfo
		BlockID     string
	}{tx, info, block.ID})
	digest := sha256.Sum256(normalized)
	out = Receipt{Transaction: Transaction{Network: r.Network, Signature: r.Hash, Slot: int64(info.Height), FeePayer: owner, FeeAtoms: strconv.FormatInt(int64(info.Fee), 10), Fingerprint: hex.EncodeToString(digest[:]), BlockHash: block.ID}, Event: event, evidence: e}
	return out, nil
}
