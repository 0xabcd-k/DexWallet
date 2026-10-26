package example

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/tests/codegen/example/internal/types"
)

func TestEchoPreservesWireTypes(t *testing.T) {
	for _, note := range []string{"", "optional value"} {
		req := &types.EchoRequest{Message: "你好", Amount: "9007199254740993.123456789012345678", Note: note}
		resp, err := NewEchoLogic(context.Background(), nil).Echo(req)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(resp)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["amount"]) != `"9007199254740993.123456789012345678"` {
			t.Fatalf("precision lost: %s", encoded)
		}
		if string(fields["items"]) != "[]" {
			t.Fatalf("array must not be null: %s", encoded)
		}
		_, hasNote := fields["note"]
		if hasNote != (note != "") {
			t.Fatalf("optional field mismatch: %s", encoded)
		}
	}
}

func TestEchoPreservesNestedZeroAndDoesNotAliasInput(t *testing.T) {
	req := &types.EchoRequest{Items: []types.SampleItem{{Kind: "counter", Label: "zero", Sequence: 0}}}
	resp, err := NewEchoLogic(context.Background(), nil).Echo(req)
	if err != nil || len(resp.Items) != 1 || resp.Items[0].Sequence != 0 {
		t.Fatalf("response: %+v, %v", resp, err)
	}
	resp.Items[0].Label = "changed"
	if req.Items[0].Label != "zero" {
		t.Fatal("response aliases input")
	}
}
