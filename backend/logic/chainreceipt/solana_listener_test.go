package chainreceipt

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

func TestSolanaBlockTokenOwnerOutsideMessage(t *testing.T) {
	data, err := os.ReadFile("testdata/solana-collection-blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	var history map[string][]struct{ Block solBlock }
	if err = json.Unmarshal(data, &history); err != nil {
		t.Fatal(err)
	}
	v := history["usdc"]
	b := v[len(v)-1].Block
	tx := &(*b.Transactions)[0]
	owner := "7Njq3pBVPhZWEoRh8w22eC2KBcGUhhCvwFFitsyTg7A5"
	tx.Transaction.AccountKeys = slices.DeleteFunc(tx.Transaction.AccountKeys, func(a account) bool { return a.Pubkey == owner })
	tx.Version = json.RawMessage("0")
	owners, err := solanaBlockOwners(b)
	if err != nil || !owners[owner][tx.Transaction.Signatures[0]] {
		t.Fatal("SPL receipt with unlisted wallet owner lost", err)
	}
	tx.Meta.PostTokenBalances = nil
	if _, err = solanaBlockOwners(b); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal("incomplete ownership data accepted", err)
	}
	tx.Version = json.RawMessage("1")
	owners, err = solanaBlockOwners(b)
	if err != nil || len(owners) != 0 {
		t.Fatal("unexpected version entered receipt queue", err)
	}
}

func TestSolanaListenerIncompleteInstructionStaysUnresolved(t *testing.T) {
	asset := chainassets.Asset{Origin: chainassets.TestFixture}
	for _, ix := range []instruction{{}, {ProgramID: TokenProgram}, {ProgramID: TokenProgram, Parsed: json.RawMessage("null")}} {
		if _, err := solanaIncomingAmount(ix, transactionResponse{}, asset, ""); !errors.Is(err, ErrInvalidEvidence) {
			t.Fatal("incomplete instruction treated as ordinary nonpayment", ix, err)
		}
	}
}
