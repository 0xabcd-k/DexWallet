package chainreceipt

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestBaseDepositFinalEvidence(t *testing.T) {
	var f struct {
		Quote                       chainassets.DepositPreview
		Wire, UserSignature, Digest string
		Evidence                    chainassets.BaseEvidence
	}
	raw, err := os.ReadFile("testdata/base-usdc-self-deposit.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	var tx types.Transaction
	wire, err := hexutil.Decode(f.Wire)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.UnmarshalBinary(wire); err != nil {
		t.Fatal(err)
	}
	a, err := f.Quote.BaseAuthorization()
	if err != nil || a.Digest().Hex() != f.Digest {
		t.Fatal("authorization digest", err)
	}
	checked, err := VerifyBaseDeposit(f.Evidence, &tx, f.Quote, f.UserSignature)
	if err != nil || checked.Event.Atoms != "11787" || checked.Transaction.FeeAtoms != "558417902115" {
		t.Fatal("finalized receipt", err, checked.Transaction.FeeAtoms)
	}
	for _, field := range []string{"order", "amount", "payer", "recipient", "expiry", "fee"} {
		q := f.Quote
		switch field {
		case "order":
			q.QuoteIdx = strings.Repeat("1", 32)
		case "amount":
			q.PayQuantity = "0.01"
		case "payer":
			q.FeePayer = "0x" + strings.Repeat("1", 40)
		case "recipient":
			q.Recipient = "0x" + strings.Repeat("1", 40)
		case "expiry":
			q.BaseValidBefore++
		case "fee":
			q.EstimatedGas = "0.000000000000000001"
			q.MaxGas = q.EstimatedGas
		}
		if _, err := VerifyBaseDeposit(f.Evidence, &tx, q, f.UserSignature); err == nil {
			t.Fatal("accepted altered", field)
		}
	}
	for _, field := range []string{"nonce", "source", "missing", "quantity", "duplicate", "l1", "price", "operator", "head", "failed"} {
		e := f.Evidence
		var receipt map[string]any
		if err = json.Unmarshal(e.Receipt, &receipt); err != nil {
			t.Fatal(err)
		}
		logs := receipt["logs"].([]any)
		auth := logs[0].(map[string]any)
		transfer := logs[1].(map[string]any)
		want := ErrInvalidEvidence
		switch field {
		case "nonce":
			auth["topics"].([]any)[2] = "0x" + strings.Repeat("0", 64)
		case "source":
			auth["topics"].([]any)[1] = "0x" + strings.Repeat("1", 64)
		case "missing":
			receipt["logs"] = logs[1:]
		case "quantity":
			transfer["data"] = "0x" + strings.Repeat("0", 63) + "1"
		case "duplicate":
			receipt["logs"] = append(logs, transfer)
		case "price":
			receipt["effectiveGasPrice"] = "0x100000000"
		case "l1":
			delete(receipt, "l1Fee")
		case "operator":
			e.OperatorFee = nil
		case "head":
			e.Finalized = json.RawMessage(`{"hash":"0x` + strings.Repeat("1", 64) + `","number":"0x1"}`)
			want = ErrUnconfirmed
		case "failed":
			receipt["status"] = "0x0"
			want = ErrExecutionFailed
		}
		e.Receipt, _ = json.Marshal(receipt)
		if _, err := VerifyBaseDeposit(e, &tx, f.Quote, f.UserSignature); !errors.Is(err, want) {
			t.Errorf("%s: %v", field, err)
		}
	}
}
