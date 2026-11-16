package chainassets

import (
	"net/url"
	"strings"
	"testing"
)

func TestReceiveCodesRoundTripWithoutAmountOrRPC(t *testing.T) {
	s, err := New(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	owners := map[Family]string{TON: tonOwner, Base: evmOwner, Solana: solOwner, TRON: tronOwner}
	for _, n := range s.Catalog() {
		for _, a := range n.Assets {
			t.Run(a.ID, func(t *testing.T) {
				first, err := s.ResolveTarget(TargetInput{Network: n.ID, AssetID: a.ID, Address: owners[n.Family]})
				if err != nil {
					t.Fatal(err)
				}
				second, err := s.ResolveTarget(TargetInput{ReceiveCode: first.ReceiveCode})
				if err != nil || second != first {
					t.Fatal(second, err)
				}
				u, _ := url.Parse(first.ReceiveCode)
				if len(u.Query()) != 4 || u.Query().Has("amount") {
					t.Fatal("receive code carries unauthorized fields", first)
				}
				if n.Family == TON && !strings.HasPrefix(first.Address, map[bool]string{false: "UQ", true: "0Q"}[n.Testnet]) {
					t.Fatal(first.Address)
				}
			})
		}
	}
}
func TestReceiveRejectsAmbiguousOrExecutablePayloads(t *testing.T) {
	s, _ := New(Config{}, nil)
	defer s.Close()
	target, _ := s.ResolveTarget(TargetInput{Network: BaseMainnet, AssetID: BaseMainnet + ":eth", Address: evmOwner})
	for _, code := range []string{target.ReceiveCode + "&amount=1", target.ReceiveCode + "&memo=order", target.ReceiveCode + "&data=0x1234", target.ReceiveCode + "&v=1", target.ReceiveCode + "#", strings.Replace(target.ReceiveCode, "v=1", "v=2", 1), strings.Replace(target.ReceiveCode, "chain-transfer?", "attacker@chain-transfer?", 1), strings.Replace(target.ReceiveCode, "chain-transfer?", "chain-transfer:443?", 1), "https://example.com/" + target.ReceiveCode, strings.Repeat("x", 2049), target.ReceiveCode + "&broken=%zz"} {
		if _, err := s.ResolveTarget(TargetInput{ReceiveCode: code}); err == nil {
			t.Fatalf("accepted bad code %q", code)
		}
	}
	if _, err := s.ResolveTarget(TargetInput{ReceiveCode: target.ReceiveCode, Address: evmOwner}); err == nil {
		t.Fatal("mixed sources")
	}
	if _, err := s.ResolveTarget(TargetInput{Network: BaseSepolia, AssetID: BaseMainnet + ":eth", Address: evmOwner}); err == nil {
		t.Fatal("mixed network asset")
	}
	testTON, _ := s.ResolveTarget(TargetInput{Network: TONTestnet, AssetID: TONTestnet + ":ton", Address: tonOwner})
	if _, err := s.ResolveTarget(TargetInput{Network: TONMainnet, AssetID: TONMainnet + ":ton", Address: testTON.Address}); err == nil {
		t.Fatal("test-only address on mainnet")
	}
}
func TestPreviewPreservesExactAmountAndNeverEnablesSubmission(t *testing.T) {
	s, _ := New(Config{}, nil)
	defer s.Close()
	input := TargetInput{Network: BaseMainnet, AssetID: BaseMainnet + ":eth", Address: evmOwner}
	for _, amount := range []string{"1", "1.000000000000000001", "9007199254740993.12"} {
		p, err := s.PreviewTransfer(input, amount)
		if err != nil || p.Amount != amount || p.CanSubmit || p.BlockReason != ChannelNotOpen {
			t.Fatal(p, err)
		}
	}
	for _, amount := range []string{"", "0", "-1", "1e3", "+1", "1.", "01", " 1", "1.0000000000000000001", "NaN", strings.Repeat("9", 80)} {
		if _, err := s.PreviewTransfer(input, amount); err == nil {
			t.Fatal("bad amount accepted", amount)
		}
	}
	sol := TargetInput{Network: SolanaMainnet, AssetID: SolanaMainnet + ":sol", Address: solOwner}
	if _, err := s.PreviewTransfer(sol, "18446744073.709551615"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewTransfer(sol, "18446744073.709551616"); err == nil {
		t.Fatal("u64 overflow accepted")
	}
	p, err := s.PreviewTransfer(input, "1.2300")
	if err != nil || p.Amount != "1.23" {
		t.Fatal(p, err)
	}
}
