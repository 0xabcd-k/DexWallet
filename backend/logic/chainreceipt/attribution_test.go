package chainreceipt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestTONCollectionEvidence(t *testing.T) {
	for _, tc := range []struct{ fixture, reference string }{
		{"ton-collection-native", "dw:5d6e1aba8402493f1d8262085dce91e5"},
		{"ton-collection-jetton", "dw:523a0df4b065f1efb694e8c8ae930fde"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			f, s := tonSample(t, tc.fixture)
			got, err := NewTONVerifier(s).Verify(context.Background(), f.Request)
			if err != nil || got.Transaction != f.Receipt.Transaction || got.Event != f.Receipt.Event {
				t.Fatal(got, err)
			}
			key := EventKey{Network: f.Request.Network, Kind: got.Event.Kind}
			raw, err := json.Marshal(s.proof)
			if err != nil {
				t.Fatal(err)
			}
			at, ref, err := receiveEvidence(key, string(raw), TONMemo)
			if err != nil || at != 1795257327 || ref != tc.reference {
				t.Fatal(at, ref, err)
			}
			if got.Event.Kind != TONJetton {
				return
			}
			parent, err := tonOneTransaction(s.proof.Parent)
			if err != nil {
				t.Fatal(err)
			}
			body, err := tonBody(parent.In)
			if err != nil {
				t.Fatal(err)
			}
			var p jetton.TransferPayload
			if err = tonPayload(body, &p); err != nil {
				t.Fatal(err)
			}
			p.ForwardPayload = cell.BeginCell().MustStoreUInt(0, 32).MustStoreStringSnake("dw:00000000000000000000000000000000").EndCell()
			other, err := tlb.ToCell(p)
			if err != nil {
				t.Fatal(err)
			}
			editTON(&s.proof.Parent, func(tx map[string]any) {
				tx["in_msg"].(map[string]any)["message_content"] = map[string]any{"body": base64.StdEncoding.EncodeToString(other.ToBOC()), "hash": base64.StdEncoding.EncodeToString(other.Hash())}
			})
			raw, err = json.Marshal(s.proof)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = receiveEvidence(key, string(raw), TONMemo); !errors.Is(err, ErrInvalidEvidence) {
				t.Fatal("payer message with a different reference accepted", err)
			}
		})
	}
}

func TestTONCollectionComment(t *testing.T) {
	const note = "dw:0123456789abcdef0123456789abcdef"
	tail := cell.BeginCell().MustStoreStringSnake(note[3:]).EndCell()
	split := cell.BeginCell().MustStoreUInt(0, 32).MustStoreStringSnake(note[:3]).MustStoreRef(tail).EndCell()
	if got, err := tonPaymentComment(split); err != nil || got != note {
		t.Fatal(got, err)
	}
	for _, b := range []*cell.Cell{
		cell.BeginCell().EndCell(),
		cell.BeginCell().MustStoreUInt(1, 32).MustStoreStringSnake(note).EndCell(),
		cell.BeginCell().MustStoreUInt(0, 32).MustStoreStringSnake("DW:" + note[3:]).EndCell(),
		cell.BeginCell().MustStoreUInt(0, 32).MustStoreStringSnake(note + " ").EndCell(),
		cell.BeginCell().MustStoreUInt(0, 32).MustStoreStringSnake(note).MustStoreUInt(0, 1).EndCell(),
		cell.BeginCell().MustStoreUInt(0, 32).MustStoreRef(tail).MustStoreRef(tail).EndCell(),
	} {
		if _, err := tonPaymentComment(b); !errors.Is(err, ErrNoReceiveRequest) {
			t.Fatal(err)
		}
	}
	for _, network := range []string{chainassets.BaseSepolia, chainassets.SolanaDevnet, chainassets.TRONNile, chainassets.TONTestnet} {
		if _, _, err := receiveEvidence(EventKey{Network: network}, `{"block":{},"transaction":{},"info":{}}`, SingleUseAddress); !errors.Is(err, ErrInvalidEvidence) {
			t.Fatal(network, err)
		}
	}
}
