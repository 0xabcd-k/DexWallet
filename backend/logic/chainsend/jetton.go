package chainsend

import (
	"context"
	"encoding/json"
	"math/big"
	"strconv"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// The principal is Proposal.Atoms. Attached/forwarded TON are separate budgets;
// response_destination is always the sending platform wallet, never arbitrary.
type JettonTransfer struct {
	Master     string `json:"master"`
	Wallet     string `json:"wallet"`
	AttachTON  string `json:"attachTon"`
	ForwardTON string `json:"forwardTon"`
	QueryID    string `json:"queryId"`
}
type JettonResolver interface {
	ResolveTONJettonWallet(context.Context, string, string) (string, error)
}

func rawTONAddress(value string) (string, error) {
	a, err := address.ParseAddr(value)
	if err != nil {
		a, err = address.ParseRawAddr(value)
	}
	if err != nil || a.Type() != address.StdAddress || a.Workchain() != 0 {
		return "", ErrInvalidRequest
	}
	return a.StringRaw(), nil
}
func coinAmount(value string, positive bool) (*big.Int, error) {
	n, ok := new(big.Int).SetString(value, 10)
	if !ok || n.Sign() < 0 || (positive && n.Sign() == 0) || n.BitLen() > 120 || n.String() != value {
		return nil, ErrInvalidRequest
	}
	return n, nil
}
func canonicalJetton(j JettonTransfer, sender, recipient string) (JettonTransfer, error) {
	if j == (JettonTransfer{}) {
		return j, nil
	}
	var err error
	j.Master, err = rawTONAddress(j.Master)
	if err != nil {
		return j, err
	}
	j.Wallet, err = rawTONAddress(j.Wallet)
	if err != nil {
		return j, err
	}
	if j.Wallet == sender || j.Wallet == recipient || j.Master == j.Wallet {
		return j, ErrInvalidRequest
	}
	attached, err := coinAmount(j.AttachTON, true)
	if err != nil {
		return j, err
	}
	forwarded, err := coinAmount(j.ForwardTON, false)
	if err != nil || forwarded.Cmp(attached) >= 0 {
		return j, ErrInvalidRequest
	}
	q, err := strconv.ParseUint(j.QueryID, 10, 64)
	if err != nil || q == 0 || strconv.FormatUint(q, 10) != j.QueryID {
		return j, ErrInvalidRequest
	}
	return j, nil
}
func jettonJSON(j JettonTransfer) string {
	if j == (JettonTransfer{}) {
		return ""
	}
	b, _ := json.Marshal(j)
	return string(b)
}
func parseJetton(raw string) (JettonTransfer, error) {
	var j JettonTransfer
	if raw == "" {
		return j, nil
	}
	if json.Unmarshal([]byte(raw), &j) != nil || jettonJSON(j) != raw {
		return j, ErrConflict
	}
	return j, nil
}
func proposalAsset(p Proposal) string {
	if p.Jetton.Master != "" {
		return chainassets.TONTestnet + ":usdt:" + p.Jetton.Master
	}
	return tonAsset
}
func (s *TON) checkJetton(ctx context.Context, p Proposal) error {
	if p.Jetton.Master == "" {
		return nil
	}
	if s.wallet.JettonMaster == "" || s.wallet.JettonMaster != p.Jetton.Master {
		return ErrInvalidRequest
	}
	resolver, ok := s.broadcaster.(JettonResolver)
	if !ok {
		return ErrInvalidRequest
	}
	derived, err := resolver.ResolveTONJettonWallet(ctx, p.Jetton.Master, s.sender)
	if err != nil {
		return err
	}
	if derived != p.Jetton.Wallet {
		return ErrConflict
	}
	return nil
}
func jettonMessage(p Proposal, sender string) (*tlb.InternalMessage, error) {
	to, _ := address.ParseRawAddr(p.Recipient)
	response, _ := address.ParseRawAddr(sender)
	target, _ := address.ParseRawAddr(p.Jetton.Wallet)
	amount, _ := coinAmount(p.Atoms, true)
	attached, _ := coinAmount(p.Jetton.AttachTON, true)
	forward, _ := coinAmount(p.Jetton.ForwardTON, false)
	q, _ := strconv.ParseUint(p.Jetton.QueryID, 10, 64)
	// The SDK convenience builder chooses a random query_id; explicit TL-B keeps
	// this authorization deterministic across signing and process recovery.
	body, err := tlb.ToCell(jetton.TransferPayload{QueryID: q, Amount: tlb.FromNanoTON(amount), Destination: to, ResponseDestination: response, ForwardTONAmount: tlb.FromNanoTON(forward), ForwardPayload: cell.BeginCell().EndCell()})
	if err != nil {
		return nil, err
	}
	return &tlb.InternalMessage{IHRDisabled: true, Bounce: true, DstAddr: target, Amount: tlb.FromNanoTON(attached), Body: body}, nil
}
