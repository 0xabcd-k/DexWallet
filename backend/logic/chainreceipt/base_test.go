package chainreceipt

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

type baseFixture struct {
	Request  BaseRequest
	Receipt  Receipt
	Evidence chainassets.BaseEvidence
}
type baseFixtureSource struct {
	catalog []chainassets.Network
	proof   chainassets.BaseEvidence
	calls   int
	err     error
}

func (s *baseFixtureSource) Catalog() []chainassets.Network { return s.catalog }
func (s *baseFixtureSource) ReadBaseEvidence(context.Context, string, string) (chainassets.BaseEvidence, error) {
	s.calls++
	return s.proof, s.err
}
func baseSample(t *testing.T, name string) (baseFixture, *baseFixtureSource) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f baseFixture
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	assets, err := chainassets.New(chainassets.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer assets.Close()
	return f, &baseFixtureSource{catalog: assets.Catalog(), proof: f.Evidence}
}
func TestBaseHistoricalSnapshotsAndCaseIdentity(t *testing.T) {
	for _, name := range []string{"base-eth", "base-usdc", "base-internal-eth"} {
		t.Run(name, func(t *testing.T) {
			f, s := baseSample(t, name)
			f.Request.Hash = "0x" + strings.ToUpper(f.Request.Hash[2:])
			v, err := NewBaseVerifier(s).Verify(context.Background(), f.Request)
			if err != nil || v.Transaction != f.Receipt.Transaction || v.Event != f.Receipt.Event {
				t.Fatalf("%+v %v", v, err)
			}
			if name == "base-usdc" && (v.Event.Instruction != 5 || v.Event.SourceOwner == v.Transaction.FeePayer) {
				t.Fatal("log identity or token owner confused with relay sender")
			}
		})
	}
}
func TestBaseInputValidationBeforeRPC(t *testing.T) {
	for _, edit := range []func(*BaseRequest){
		func(r *BaseRequest) { r.Hash = "fake" }, func(r *BaseRequest) { r.Recipient = zeroEVMAddress }, func(r *BaseRequest) { r.Atoms = "01" }, func(r *BaseRequest) { r.Atoms = "1.2" }, func(r *BaseRequest) { r.Atoms = new(big.Int).Lsh(big.NewInt(1), 256).String() }, func(r *BaseRequest) { r.Network = chainassets.BaseMainnet }, func(r *BaseRequest) { r.LogIndex = nil }, func(r *BaseRequest) { i := -1; r.LogIndex = &i },
	} {
		f, s := baseSample(t, "base-usdc")
		edit(&f.Request)
		_, err := NewBaseVerifier(s).Verify(context.Background(), f.Request)
		if !errors.Is(err, ErrInvalidRequest) || s.calls != 0 {
			t.Fatal(err, s.calls)
		}
	}
}
func TestBaseRejectsWrongAndUnfinalizedEvidence(t *testing.T) {
	tests := []struct {
		name string
		edit func(*chainassets.BaseEvidence)
		want error
	}{
		{"absent", func(e *chainassets.BaseEvidence) { e.Receipt = json.RawMessage(`null`) }, ErrUnconfirmed},
		{"unfinalized", func(e *chainassets.BaseEvidence) {
			mutate(&e.Finalized, func(m map[string]any) { m["number"] = "0x1" })
		}, ErrUnconfirmed},
		{"wrong canonical block", func(e *chainassets.BaseEvidence) {
			mutate(&e.Block, func(m map[string]any) { m["hash"] = "0x" + strings.Repeat("a", 64) })
		}, ErrInvalidEvidence},
		{"wrong chain", func(e *chainassets.BaseEvidence) {
			mutate(&e.Transaction, func(m map[string]any) { m["chainId"] = "0x2105" })
		}, ErrInvalidEvidence},
		{"wrong hash", func(e *chainassets.BaseEvidence) {
			mutate(&e.Transaction, func(m map[string]any) { m["hash"] = "0x" + strings.Repeat("a", 64) })
		}, ErrInvalidEvidence},
		{"wrong tx index", func(e *chainassets.BaseEvidence) {
			mutate(&e.Receipt, func(m map[string]any) { m["transactionIndex"] = "0x0" })
		}, ErrInvalidEvidence},
		{"wrong membership", func(e *chainassets.BaseEvidence) {
			mutate(&e.Block, func(m map[string]any) { m["transactions"] = []string{} })
		}, ErrInvalidEvidence},
		{"failed", func(e *chainassets.BaseEvidence) { mutate(&e.Receipt, func(m map[string]any) { m["status"] = "0x0" }) }, ErrExecutionFailed},
		{"missing status", func(e *chainassets.BaseEvidence) { mutate(&e.Receipt, func(m map[string]any) { delete(m, "status") }) }, ErrInvalidEvidence},
		{"missing l1 fee", func(e *chainassets.BaseEvidence) { mutate(&e.Receipt, func(m map[string]any) { delete(m, "l1Fee") }) }, ErrInvalidEvidence},
		{"missing operator fee", func(e *chainassets.BaseEvidence) { e.OperatorFee = json.RawMessage(`null`) }, ErrInvalidEvidence},
		{"bad operator ABI", func(e *chainassets.BaseEvidence) { e.OperatorFee = json.RawMessage(`"0x0"`) }, ErrInvalidEvidence},
		{"missing logs", func(e *chainassets.BaseEvidence) { mutate(&e.Receipt, func(m map[string]any) { delete(m, "logs") }) }, ErrInvalidEvidence},
		{"removed", func(e *chainassets.BaseEvidence) {
			mutate(&e.Receipt, func(m map[string]any) { baseTransferLog(m)["removed"] = true })
		}, ErrInvalidEvidence},
		{"duplicate index", func(e *chainassets.BaseEvidence) {
			mutate(&e.Receipt, func(m map[string]any) { m["logs"].([]any)[0].(map[string]any)["logIndex"] = "0x5" })
		}, ErrInvalidEvidence},
		{"wrong log tx", func(e *chainassets.BaseEvidence) {
			mutate(&e.Receipt, func(m map[string]any) { baseTransferLog(m)["transactionHash"] = "0x" + strings.Repeat("a", 64) })
		}, ErrInvalidEvidence},
		{"wrong token", func(e *chainassets.BaseEvidence) {
			mutate(&e.Receipt, func(m map[string]any) { baseTransferLog(m)["address"] = m["from"] })
		}, ErrInvalidEvidence},
		{"wrong topic", func(e *chainassets.BaseEvidence) {
			mutate(&e.Receipt, func(m map[string]any) { baseTransferLog(m)["topics"].([]any)[0] = "0x" + strings.Repeat("0", 64) })
		}, ErrInvalidEvidence},
		{"bad address padding", func(e *chainassets.BaseEvidence) {
			mutate(&e.Receipt, func(m map[string]any) { baseTransferLog(m)["topics"].([]any)[2] = "0x" + strings.Repeat("1", 64) })
		}, ErrInvalidEvidence},
		{"wrong recipient", func(e *chainassets.BaseEvidence) {
			mutate(&e.Receipt, func(m map[string]any) { l := baseTransferLog(m); l["topics"].([]any)[2] = l["topics"].([]any)[1] })
		}, ErrInvalidEvidence},
		{"wrong amount", func(e *chainassets.BaseEvidence) {
			mutate(&e.Receipt, func(m map[string]any) { baseTransferLog(m)["data"] = "0x" + strings.Repeat("0", 63) + "1" })
		}, ErrInvalidEvidence},
		{"mint", func(e *chainassets.BaseEvidence) {
			mutate(&e.Receipt, func(m map[string]any) { baseTransferLog(m)["topics"].([]any)[1] = "0x" + strings.Repeat("0", 64) })
		}, ErrInvalidEvidence},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s := baseSample(t, "base-usdc")
			tt.edit(&s.proof)
			_, err := NewBaseVerifier(s).Verify(context.Background(), f.Request)
			if !errors.Is(err, tt.want) {
				t.Fatal(err)
			}
		})
	}
	f, s := baseSample(t, "base-usdc")
	index := 1
	f.Request.LogIndex = &index
	if _, err := NewBaseVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, ErrUnsupported) {
		t.Fatal("array position treated as logIndex", err)
	}
	f, s = baseSample(t, "base-usdc")
	s.err = errors.New("offline")
	if _, err := NewBaseVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, s.err) {
		t.Fatal(err)
	}
}
func baseTransferLog(m map[string]any) map[string]any { return m["logs"].([]any)[1].(map[string]any) }
func TestBaseFullUint256AndFeeComponents(t *testing.T) {
	f, s := baseSample(t, "base-usdc")
	f.Request.Atoms = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)).String()
	mutate(&s.proof.Receipt, func(m map[string]any) { baseTransferLog(m)["data"] = "0x" + strings.Repeat("f", 64) })
	s.proof.OperatorFee = json.RawMessage(`"0x` + strings.Repeat("0", 62) + `7b"`)
	v, err := NewBaseVerifier(s).Verify(context.Background(), f.Request)
	if err != nil || v.Event.Atoms != f.Request.Atoms || v.Transaction.OperatorFeeAtoms != "123" || v.Transaction.FeeAtoms != "617208000319" {
		t.Fatal(v, err)
	}
}
func TestBaseNativeDoesNotInferInternalTransfers(t *testing.T) {
	for _, edit := range []func(map[string]any){func(m map[string]any) { m["input"] = "0x1234" }, func(m map[string]any) { m["value"] = "0x0" }} {
		f, s := baseSample(t, "base-eth")
		mutate(&s.proof.Transaction, edit)
		if _, err := NewBaseVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, ErrUnsupported) {
			t.Fatal(err)
		}
	}
	f, s := baseSample(t, "base-eth")
	f.Request.Atoms = "1"
	if _, err := NewBaseVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatal(err)
	}
}
