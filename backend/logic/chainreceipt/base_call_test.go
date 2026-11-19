package chainreceipt

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

func (s *baseFixtureSource) ReadBaseCallEvidence(context.Context, string, string) (chainassets.BaseEvidence, error) {
	s.calls++
	return s.proof, s.err
}

func TestBaseInternalCallIdentityAndPayer(t *testing.T) {
	f, s := baseSample(t, "base-internal-eth-fixture")
	v, err := NewBaseVerifier(s).Verify(context.Background(), f.Request)
	if err != nil {
		t.Fatal(err)
	}
	if v.Event.Kind != EVMCall || v.Event.Instruction != 6 || v.Event.Atoms != "1000000000000" || v.Event.SourceAccount != "0xecd14f7586f5053cecae148ae26309c2277d86a8" || v.Event.SourceOwner == v.Transaction.FeePayer || len(v.traceFingerprint) != 64 {
		t.Fatal(v)
	}
	// The same transaction may have already been recorded via a token log.
	logIndex := 5
	token := f.Request
	token.CallIndex, token.LogIndex, token.AssetID, token.Atoms = nil, &logIndex, "base-sepolia:usdc", "10000"
	log, err := NewBaseVerifier(s).Verify(context.Background(), token)
	if err != nil || log.Transaction != v.Transaction || log.traceFingerprint != "" {
		t.Fatal(log, err)
	}
	// Moving finality, hex case and optional empty output do not change identity.
	mutate(&s.proof.Finalized, func(m map[string]any) { m["number"] = "0xfffffff" })
	mutate(&s.proof.CallTrace, func(m map[string]any) {
		m["from"] = "0x" + strings.ToUpper(m["from"].(string)[2:])
		delete(m, "output")
	})
	again, err := NewBaseVerifier(s).Verify(context.Background(), f.Request)
	if err != nil || again.Event != v.Event || again.Transaction != v.Transaction || again.traceFingerprint != v.traceFingerprint {
		t.Fatal(again, err)
	}
}

func TestBaseCallKeepsFailedCreationInPosition(t *testing.T) {
	f, s := baseSample(t, "base-internal-eth-fixture")
	mutate(&s.proof.CallTrace, func(m map[string]any) { c := callAt(m, 0); c["type"] = "CREATE"; delete(c, "to") })
	v, err := NewBaseVerifier(s).Verify(context.Background(), f.Request)
	if err != nil || v.Event.Instruction != 6 {
		t.Fatal(v, err)
	}
	i := 2
	f.Request.CallIndex = &i
	if _, err = NewBaseVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, ErrExecutionFailed) {
		t.Fatal(err)
	}
}

func callAt(m map[string]any, path ...int) map[string]any {
	for _, i := range path {
		m = m["calls"].([]any)[i].(map[string]any)
	}
	return m
}

func TestBaseInternalCallRejectsUnprovenTransfers(t *testing.T) {
	for _, tt := range []struct {
		name string
		edit func(*BaseRequest, *chainassets.BaseEvidence)
		want error
	}{
		{"missing trace", func(_ *BaseRequest, e *chainassets.BaseEvidence) { e.CallTrace = nil }, ErrInvalidEvidence},
		{"root only", func(_ *BaseRequest, e *chainassets.BaseEvidence) {
			mutate(&e.CallTrace, func(m map[string]any) { delete(m, "calls") })
		}, ErrInvalidEvidence},
		{"absent index", func(r *BaseRequest, _ *chainassets.BaseEvidence) { i := 100; r.CallIndex = &i }, ErrInvalidEvidence},
		{"reverted ancestor", func(r *BaseRequest, _ *chainassets.BaseEvidence) { i := 2; r.CallIndex = &i }, ErrExecutionFailed},
		{"failed selected", func(_ *BaseRequest, e *chainassets.BaseEvidence) {
			mutate(&e.CallTrace, func(m map[string]any) { callAt(m, 2, 0, 0)["error"] = "out of gas" })
		}, ErrExecutionFailed},
		{"delegate is not a transfer", func(r *BaseRequest, _ *chainassets.BaseEvidence) { i := 5; r.CallIndex = &i }, ErrUnsupported},
		{"root calldata mismatch", func(_ *BaseRequest, e *chainassets.BaseEvidence) {
			mutate(&e.CallTrace, func(m map[string]any) { m["input"] = "0x" })
		}, ErrInvalidEvidence},
		{"root payer mismatch", func(_ *BaseRequest, e *chainassets.BaseEvidence) {
			mutate(&e.CallTrace, func(m map[string]any) { m["from"] = "0x" + strings.Repeat("a", 40) })
		}, ErrInvalidEvidence},
		{"root fee mismatch", func(_ *BaseRequest, e *chainassets.BaseEvidence) {
			mutate(&e.CallTrace, func(m map[string]any) { m["gasUsed"] = "0x1" })
		}, ErrInvalidEvidence},
		{"root failed despite successful receipt", func(_ *BaseRequest, e *chainassets.BaseEvidence) {
			mutate(&e.CallTrace, func(m map[string]any) { m["error"] = "execution reverted" })
		}, ErrInvalidEvidence},
		{"wrong execution account", func(_ *BaseRequest, e *chainassets.BaseEvidence) {
			mutate(&e.CallTrace, func(m map[string]any) { callAt(m, 2, 0, 0)["from"] = m["to"] })
		}, ErrInvalidEvidence},
		{"wrong amount", func(r *BaseRequest, _ *chainassets.BaseEvidence) { r.Atoms = "1" }, ErrInvalidEvidence},
		{"wrong recipient", func(r *BaseRequest, _ *chainassets.BaseEvidence) { r.Recipient = "0x" + strings.Repeat("a", 40) }, ErrInvalidEvidence},
		{"zero value", func(_ *BaseRequest, e *chainassets.BaseEvidence) {
			mutate(&e.CallTrace, func(m map[string]any) { callAt(m, 2, 0, 0)["value"] = "0x0" })
		}, ErrInvalidEvidence},
		{"static ancestry", func(_ *BaseRequest, e *chainassets.BaseEvidence) {
			mutate(&e.CallTrace, func(m map[string]any) { callAt(m, 2)["type"] = "STATICCALL" })
		}, ErrInvalidEvidence},
		{"missing root gas", func(_ *BaseRequest, e *chainassets.BaseEvidence) {
			mutate(&e.CallTrace, func(m map[string]any) { delete(m, "gas") })
		}, ErrInvalidEvidence},
		{"malformed sibling", func(_ *BaseRequest, e *chainassets.BaseEvidence) {
			mutate(&e.CallTrace, func(m map[string]any) { callAt(m, 2, 0, 1)["value"] = "1.0" })
		}, ErrInvalidEvidence},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, s := baseSample(t, "base-internal-eth-fixture")
			tt.edit(&f.Request, &s.proof)
			_, err := NewBaseVerifier(s).Verify(context.Background(), f.Request)
			if !errors.Is(err, tt.want) {
				t.Fatal(err, tt.want)
			}
		})
	}
}

func TestBaseCallPositionInputAndFullTraceFingerprint(t *testing.T) {
	for _, edit := range []func(*BaseRequest){
		func(r *BaseRequest) { i := 0; r.CallIndex = &i },
		func(r *BaseRequest) { i := -1; r.CallIndex = &i },
		func(r *BaseRequest) { i := 5; r.LogIndex = &i },
		func(r *BaseRequest) { r.AssetID = "base-sepolia:usdc" },
	} {
		f, s := baseSample(t, "base-internal-eth-fixture")
		edit(&f.Request)
		_, err := NewBaseVerifier(s).Verify(context.Background(), f.Request)
		if !errors.Is(err, ErrInvalidRequest) || s.calls != 0 {
			t.Fatal(err, s.calls)
		}
	}
	f, s := baseSample(t, "base-internal-eth-fixture")
	v, err := NewBaseVerifier(s).Verify(context.Background(), f.Request)
	if err != nil {
		t.Fatal(err)
	}
	mutate(&s.proof.CallTrace, func(m map[string]any) { callAt(m, 2, 0, 1)["value"] = "0x15" })
	w, err := NewBaseVerifier(s).Verify(context.Background(), f.Request)
	if err != nil || v.Event != w.Event || v.Transaction != w.Transaction || v.traceFingerprint == w.traceFingerprint {
		t.Fatal(w, err)
	}
	s.err = errors.New("archive unavailable")
	if _, err = NewBaseVerifier(s).Verify(context.Background(), f.Request); !errors.Is(err, s.err) {
		t.Fatal(err)
	}
	// Explicit internal mode never falls back to outer value.
	f.Request.CallIndex = nil
	if _, err = verifyBase(f.Request, chainassets.Asset{ID: f.Request.AssetID, Origin: chainassets.Native, Decimals: 18}, f.Evidence); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}
