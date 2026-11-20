// Package chainreceipt verifies chain facts. A receipt is not an order claim,
// a ledger credit, or permission to settle any product user's payment.
package chainreceipt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/0xabcd-k/DexWallet/backend/internal/solanaspl"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/mr-tron/base58"
)

const SystemProgram = solanaspl.SystemProgram
const TokenProgram = solanaspl.Program
const Finalized = "finalized"
const SolanaInstruction = "solana_instruction"

var (
	ErrInvalidRequest  = errors.New("invalid receipt request")
	ErrUnconfirmed     = errors.New("transaction not proven finalized; absence is not proof of failure")
	ErrExecutionFailed = errors.New("finalized transaction execution failed")
	ErrInvalidEvidence = errors.New("invalid or mismatched transaction evidence")
	ErrUnsupported     = errors.New("unsupported transfer shape")
	ErrConflict        = errors.New("recorded chain facts conflict with observation")
	ErrOutcomeUnknown  = errors.New("receipt commit outcome unknown; recover the same event")
)

type Request struct {
	Network     string `json:"network"`
	Signature   string `json:"signature"`
	Instruction int    `json:"instruction"`
	AssetID     string `json:"assetId"`
	Recipient   string `json:"recipient"`
	Atoms       string `json:"atoms"`
}
type Transaction struct {
	Network           string `json:"network"`
	Signature         string `json:"signature"`
	Slot              int64  `json:"slot"`
	FeePayer          string `json:"feePayer"`
	FeeAtoms          string `json:"feeAtoms"`
	Fingerprint       string `json:"fingerprint"`
	BlockHash         string `json:"blockHash,omitempty"`
	ExecutionFeeAtoms string `json:"executionFeeAtoms,omitempty"`
	L1FeeAtoms        string `json:"l1FeeAtoms,omitempty"`
	OperatorFeeAtoms  string `json:"operatorFeeAtoms,omitempty"`
}
type Event struct {
	Kind             string `json:"kind"`
	Instruction      int    `json:"instruction"`
	AssetID          string `json:"assetId"`
	ProgramID        string `json:"programId"`
	Mint             string `json:"mint"`
	Decimals         int    `json:"decimals"`
	SourceAccount    string `json:"sourceAccount"`
	SourceOwner      string `json:"sourceOwner"`
	RecipientAccount string `json:"recipientAccount"`
	RecipientOwner   string `json:"recipientOwner"`
	Atoms            string `json:"atoms"`
}
type Receipt struct {
	Transaction      Transaction `json:"transaction"`
	Event            Event       `json:"event"`
	evidence         any
	traceFingerprint string
	callTrace        json.RawMessage
}
type Source interface {
	Catalog() []chainassets.Network
	ReadSolanaEvidence(context.Context, string, string) (chainassets.SolanaEvidence, error)
}
type Verifier struct{ source Source }

func NewVerifier(source Source) *Verifier { return &Verifier{source} }
func key(raw string, size int) bool {
	if len(raw) > 88 {
		return false
	}
	b, err := base58.Decode(raw)
	return err == nil && len(b) == size
}
func atoms(raw string) bool {
	n, err := strconv.ParseUint(raw, 10, 64)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == raw
}
func bad(detail string) error       { return fmt.Errorf("%w: %s", ErrInvalidEvidence, detail) }
func null(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func (v *Verifier) solanaAsset(r Request) (chainassets.Asset, error) {
	var asset chainassets.Asset
	for _, n := range v.source.Catalog() {
		if n.ID == r.Network && n.Family == chainassets.Solana {
			for _, a := range n.Assets {
				if a.ID == r.AssetID {
					asset = a
					break
				}
			}
		}
	}
	if asset.ID == "" {
		return asset, ErrInvalidRequest
	}
	return asset, nil
}

func (v *Verifier) Verify(ctx context.Context, r Request) (Receipt, error) {
	if r.Instruction < 0 || !key(r.Signature, 64) || !key(r.Recipient, 32) || !atoms(r.Atoms) {
		return Receipt{}, ErrInvalidRequest
	}
	asset, err := v.solanaAsset(r)
	if err != nil {
		return Receipt{}, err
	}
	evidence, err := v.source.ReadSolanaEvidence(ctx, r.Network, r.Signature)
	if err != nil {
		return Receipt{}, fmt.Errorf("read receipt %s/%s: %w", r.Network, r.Signature, err)
	}
	return verify(r, asset, evidence)
}

type account struct {
	Pubkey   string
	Signer   bool
	Writable bool
}
type tokenAmount struct {
	Amount   string
	Decimals int
}
type tokenBalance struct {
	AccountIndex  *int
	Mint          string
	Owner         string
	ProgramID     string
	UITokenAmount tokenAmount
}
type instruction struct {
	ProgramID string
	Parsed    json.RawMessage
}
type parsedTransfer struct {
	Type string
	Info struct {
		Source      string
		Destination string
		Authority   string
		Mint        string
		Lamports    *uint64
		TokenAmount tokenAmount
	}
}
type transactionResponse struct {
	Slot    int64
	Version json.RawMessage
	Meta    *struct {
		Err               json.RawMessage
		Fee               *uint64
		PreTokenBalances  []tokenBalance
		PostTokenBalances []tokenBalance
	}
	Transaction struct {
		Signatures []string
		Message    struct {
			AccountKeys  []account
			Instructions []instruction
		}
	}
}
type statusResponse struct {
	Context struct{ Slot int64 }
	Value   []*struct {
		Slot               int64
		Err                json.RawMessage
		ConfirmationStatus string
	}
}

func verify(r Request, asset chainassets.Asset, evidence chainassets.SolanaEvidence) (Receipt, error) {
	var out Receipt
	if len(evidence.Transaction) == 0 || len(evidence.Status) == 0 {
		return out, bad("missing response")
	}
	if null(evidence.Transaction) {
		return out, ErrUnconfirmed
	}
	var tx transactionResponse
	var statuses statusResponse
	if json.Unmarshal(evidence.Transaction, &tx) != nil || json.Unmarshal(evidence.Status, &statuses) != nil {
		return out, bad("invalid response fields")
	}
	if tx.Meta == nil || tx.Meta.Fee == nil || len(tx.Meta.Err) == 0 || tx.Slot <= 0 || len(tx.Transaction.Signatures) == 0 || tx.Transaction.Signatures[0] != r.Signature {
		return out, bad("missing metadata or wrong signature")
	}
	if string(tx.Version) != `"legacy"` && string(tx.Version) != "0" {
		return out, ErrUnsupported
	}
	if len(statuses.Value) != 1 {
		return out, bad("missing signature status")
	}
	st := statuses.Value[0]
	if st == nil {
		return out, ErrUnconfirmed
	}
	if len(st.Err) == 0 || st.Slot != tx.Slot || statuses.Context.Slot < tx.Slot {
		return out, bad("inconsistent status slot")
	}
	if st.ConfirmationStatus != Finalized {
		return out, ErrUnconfirmed
	}
	if null(tx.Meta.Err) != null(st.Err) {
		return out, bad("inconsistent execution status")
	}
	if !null(tx.Meta.Err) {
		// Solana transaction errors are enum strings or tagged objects. A
		// malformed RPC scalar is not evidence that execution failed.
		for _, raw := range []json.RawMessage{tx.Meta.Err, st.Err} {
			value := bytes.TrimSpace(raw)
			if value[0] != '"' && value[0] != '{' {
				return out, bad("invalid execution error")
			}
		}
		return out, ErrExecutionFailed
	}
	keys := tx.Transaction.Message.AccountKeys
	if len(keys) == 0 || !keys[0].Signer || !keys[0].Writable {
		return out, bad("missing fee payer")
	}
	index := map[string]int{}
	signers := 0
	for i, k := range keys {
		if !key(k.Pubkey, 32) {
			return out, bad("bad account key")
		}
		if _, ok := index[k.Pubkey]; ok {
			return out, bad("duplicate account key")
		}
		index[k.Pubkey] = i
		if k.Signer {
			signers++
		}
	}
	if signers != len(tx.Transaction.Signatures) {
		return out, bad("signature count differs from message")
	}
	for _, signature := range tx.Transaction.Signatures {
		if !key(signature, 64) {
			return out, bad("invalid signature bytes")
		}
	}
	instructions := tx.Transaction.Message.Instructions
	if r.Instruction >= len(instructions) {
		return out, ErrUnsupported
	}
	ix := instructions[r.Instruction]
	if _, ok := index[ix.ProgramID]; !ok {
		return out, bad("instruction program absent from accounts")
	}
	var transfer parsedTransfer
	if json.Unmarshal(ix.Parsed, &transfer) != nil {
		return out, ErrUnsupported
	}
	info := transfer.Info
	sourceIndex, sourceOK := index[info.Source]
	destIndex, destOK := index[info.Destination]
	if !sourceOK || !destOK || sourceIndex == destIndex || !keys[sourceIndex].Writable || !keys[destIndex].Writable {
		return out, bad("transfer accounts absent, same, or readonly")
	}
	event := Event{Kind: SolanaInstruction, Instruction: r.Instruction, AssetID: asset.ID, ProgramID: ix.ProgramID, Mint: asset.Contract, Decimals: asset.Decimals, SourceAccount: info.Source, RecipientAccount: info.Destination}
	if asset.Origin == chainassets.Native {
		if ix.ProgramID != SystemProgram || transfer.Type != "transfer" {
			return out, ErrUnsupported
		}
		if info.Lamports == nil || !keys[sourceIndex].Signer {
			return out, bad("invalid native amount or authority")
		}
		event.Atoms = strconv.FormatUint(*info.Lamports, 10)
		event.SourceOwner = info.Source
		event.RecipientOwner = info.Destination
	} else {
		if ix.ProgramID != TokenProgram || transfer.Type != "transferChecked" {
			return out, ErrUnsupported
		}
		authority, ok := index[info.Authority]
		if !ok || !keys[authority].Signer || info.Mint != asset.Contract || info.TokenAmount.Decimals != asset.Decimals {
			return out, bad("token mint, precision or authority")
		}
		if _, ok := index[info.Mint]; !ok {
			return out, bad("mint absent from message")
		}
		source, err := balanceOwner(tx.Meta.PreTokenBalances, sourceIndex, len(keys), asset)
		if err != nil {
			return out, err
		}
		dest, err := balanceOwner(tx.Meta.PostTokenBalances, destIndex, len(keys), asset)
		if err != nil {
			return out, err
		}
		event.SourceOwner = source
		event.RecipientOwner = dest
		event.Atoms = info.TokenAmount.Amount
	}
	if !atoms(event.Atoms) || event.Atoms != r.Atoms || event.RecipientOwner != r.Recipient || event.SourceOwner == event.RecipientOwner {
		return out, bad("amount or recipient differs, or self transfer")
	}
	// Compare retained parsed financial facts across observations, including other
	// instructions. JSON key order and status observation time are immaterial.
	normalized, _ := json.Marshal(tx)
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.UseNumber()
	var object any
	if err := decoder.Decode(&object); err != nil {
		return out, bad("cannot fingerprint evidence")
	}
	normalized, err := json.Marshal(object)
	if err != nil {
		return out, bad("cannot fingerprint evidence")
	}
	digest := sha256.Sum256(normalized)
	out = Receipt{Transaction: Transaction{Network: r.Network, Signature: r.Signature, Slot: tx.Slot, FeePayer: keys[0].Pubkey, FeeAtoms: strconv.FormatUint(*tx.Meta.Fee, 10), Fingerprint: fmt.Sprintf("%x", digest)}, Event: event, evidence: evidence}
	return out, nil
}
func balanceOwner(balances []tokenBalance, wanted, keyCount int, asset chainassets.Asset) (string, error) {
	seen := map[int]bool{}
	owner := ""
	for _, b := range balances {
		if b.AccountIndex == nil || *b.AccountIndex < 0 || *b.AccountIndex >= keyCount || seen[*b.AccountIndex] {
			return "", bad("missing/duplicate/out-of-range token account index")
		}
		seen[*b.AccountIndex] = true
		if *b.AccountIndex != wanted {
			continue
		}
		if b.Mint != asset.Contract || b.ProgramID != TokenProgram || b.UITokenAmount.Decimals != asset.Decimals || !key(b.Owner, 32) {
			return "", bad("transaction-time token identity differs")
		}
		owner = b.Owner
	}
	if owner == "" {
		return "", bad("transaction-time token owner unavailable")
	}
	return owner, nil
}
