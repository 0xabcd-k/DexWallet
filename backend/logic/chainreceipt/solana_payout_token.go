package chainreceipt

import (
	"encoding/json"

	"math"
	"strconv"

	"github.com/0xabcd-k/DexWallet/backend/internal/solanaspl"
)

type ataCreation struct {
	Type string
	Info struct{ Account, Mint, Source, SystemProgram, TokenProgram, Wallet string }
}

func verifySPLPayout(t solanaspl.Transfer, receipt Receipt, blockhash string, raw json.RawMessage) (int64, error) {
	var tx struct {
		Version string
		Meta    struct {
			PreBalances, PostBalances           []*uint64
			PreTokenBalances, PostTokenBalances []tokenBalance
		}
		Transaction struct {
			Signatures []string
			Message    struct {
				RecentBlockhash string
				AccountKeys     []account
				Instructions    []instruction
			}
		}
	}
	if err := json.Unmarshal(raw, &tx); err != nil {
		return 0, bad("malformed SPL payout transaction")
	}
	msg := tx.Transaction.Message
	keys := t.Keys()
	meta := tx.Meta
	if tx.Version != "legacy" || len(tx.Transaction.Signatures) != 1 || msg.RecentBlockhash != blockhash || len(msg.Instructions) != 1+t.TransferIndex() || len(msg.AccountKeys) != len(keys) ||
		receipt.Transaction.FeePayer != t.Sender || receipt.Event.SourceAccount != t.SourceATA || receipt.Event.RecipientAccount != t.DestinationATA || receipt.Event.SourceOwner != t.Sender {
		return 0, bad("SPL payout differs from signed intent")
	}
	if len(meta.PreBalances) != len(keys) || len(meta.PostBalances) != len(keys) {
		return 0, bad("SPL native balance array length")
	}
	source, dest := 1, 2
	if t.CreateATA {
		source, dest = 2, 1
		var create ataCreation
		ix := msg.Instructions[0]
		if ix.ProgramID != solanaspl.AssociatedProgram || json.Unmarshal(ix.Parsed, &create) != nil || create.Type != "createIdempotent" || create.Info.Account != t.DestinationATA || create.Info.Wallet != t.Recipient || create.Info.Source != t.Sender || create.Info.Mint != t.Mint || create.Info.SystemProgram != SystemProgram || create.Info.TokenProgram != TokenProgram {
			return 0, bad("unexpected associated account creation")
		}
	}
	for i, k := range keys {
		a := msg.AccountKeys[i]
		if a.Pubkey != k || a.Signer != (i == 0) || a.Writable != (i < 3) {
			return 0, bad("SPL account order or permissions differ")
		}
		if meta.PreBalances[i] == nil || meta.PostBalances[i] == nil {
			return 0, bad("missing SOL balance")
		}
		if i != 0 && i != dest && *meta.PreBalances[i] != *meta.PostBalances[i] {
			return 0, bad("unexpected SOL movement")
		}
	}
	if err := t.CheckTokenDeltas(source, dest, meta.PreTokenBalances, meta.PostTokenBalances); err != nil {
		return 0, bad(err.Error())
	}
	before, after := *meta.PreBalances[dest], *meta.PostBalances[dest]
	if after < before || after-before > math.MaxInt64 || (!t.CreateATA && after != before) {
		return 0, bad("unexpected destination ATA funding")
	}
	funding := after - before
	fee, err := strconv.ParseUint(receipt.Transaction.FeeAtoms, 10, 64)
	payerBefore, payerAfter := *meta.PreBalances[0], *meta.PostBalances[0]
	if err != nil || payerBefore < payerAfter || payerBefore-payerAfter < fee || payerBefore-payerAfter-fee != funding {
		return 0, bad("fee and ATA funding do not reconcile with payer balance")
	}
	return int64(funding), nil
}
