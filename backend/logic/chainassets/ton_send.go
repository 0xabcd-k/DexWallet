package chainassets

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func (s *Service) tonTestnet() (Network, Endpoint, error) {
	for _, n := range s.networks {
		if n.ID == TONTestnet && n.Enabled {
			return n, s.endpoints[n.ID], nil
		}
	}
	return Network{}, Endpoint{}, fmt.Errorf("TON testnet endpoint not enabled")
}
func checkTONTestnet(raw json.RawMessage) error {
	var r struct {
		OK     bool `json:"ok"`
		Result struct {
			Init struct {
				Root string `json:"root_hash"`
			} `json:"init"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &r) != nil || !r.OK || r.Result.Init.Root != TONTestnetZeroState {
		return invalid("TON testnet zero-state mismatch")
	}
	return nil
}

// SubmitTON is deliberately testnet-only. Acceptance is not chain confirmation;
// all node/transport errors leave the sending executor's original message unknown.
func (s *Service) SubmitTON(ctx context.Context, network string, boc []byte) error {
	if network != TONTestnet {
		return fmt.Errorf("TON sending only supports testnet")
	}
	if len(boc) == 0 || len(boc) > 64<<10 {
		return invalid("invalid TON send message size")
	}
	roots, err := cell.FromBOCMultiRoot(boc)
	if err != nil || len(roots) != 1 {
		return invalid("invalid TON send BoC")
	}
	var ext tlb.ExternalMessage
	if tlb.LoadFromCell(&ext, roots[0].BeginParse()) != nil || ext.StateInit != nil || ext.Body == nil || ext.DstAddr == nil || ext.DstAddr.Workchain() != 0 {
		return invalid("unsupported TON send envelope")
	}
	n, e, err := s.tonTestnet()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	read := s.tonReader(ctx, n, e)
	var raw json.RawMessage
	if err = read("/api/v2/getMasterchainInfo", nil, nil, &raw); err != nil {
		return err
	}
	if err = checkTONTestnet(raw); err != nil {
		return err
	}
	if err = read("/api/v2/sendBocReturnHash", nil, map[string]string{"boc": base64.StdEncoding.EncodeToString(boc)}, &raw); err != nil {
		return err
	}
	var response struct {
		OK     bool `json:"ok"`
		Result struct {
			Hash string `json:"hash"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &response) != nil || !response.OK {
		return invalid("TON broadcast was not acknowledged")
	}
	hash, err := base64.StdEncoding.DecodeString(response.Result.Hash)
	if err != nil || hex.EncodeToString(hash) != hex.EncodeToString(roots[0].Hash()) {
		return invalid("TON broadcast acknowledgment hash mismatch")
	}
	return nil
}

// Lookup is by the signed BODY hash because indexers may normalize the external
// envelope hash. It returns raw sender/recipient evidence; the receipt verifier
// independently binds that body, addresses, amounts and finalized block facts.
func (s *Service) ReadTONSendEvidence(ctx context.Context, network, bodyHash string) (TONEvidence, error) {
	return s.readTONSendEvidence(ctx, network, bodyHash, "", "")
}
func (s *Service) ReadTONJettonSendEvidence(ctx context.Context, network, bodyHash, master, recipient string) (TONEvidence, error) {
	if master == "" {
		return TONEvidence{}, invalid("missing Jetton master")
	}
	return s.readTONSendEvidence(ctx, network, bodyHash, master, recipient)
}
func (s *Service) readTONSendEvidence(ctx context.Context, network, bodyHash, master, recipient string) (TONEvidence, error) {
	var out TONEvidence
	if network != TONTestnet || len(bodyHash) != 64 {
		return out, fmt.Errorf("invalid TON send lookup")
	}
	if _, err := hex.DecodeString(bodyHash); err != nil {
		return out, err
	}
	n, e, err := s.tonTestnet()
	if err != nil {
		return out, err
	}
	if master != "" {
		master, recipient, err = allowedTONJetton(n, master, recipient)
		if err != nil {
			return out, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	read := s.tonReader(ctx, n, e)
	if err = read("/api/v2/getMasterchainInfo", nil, nil, &out.Masterchain); err != nil {
		return out, err
	}
	if err = checkTONTestnet(out.Masterchain); err != nil {
		return out, err
	}
	if err = read("/api/v3/transactionsByMessage", url.Values{"body_hash": {strings.ToLower(bodyHash)}, "direction": {"in"}, "limit": {"2"}}, nil, &out.Parent); err != nil {
		return out, err
	}
	var sending struct {
		Transactions *[]struct {
			Hash    string `json:"hash"`
			Account string `json:"account"`
		} `json:"transactions"`
	}
	if json.Unmarshal(out.Parent, &sending) != nil || sending.Transactions == nil || len(*sending.Transactions) > 1 {
		return out, invalid("ambiguous TON send transactions")
	}
	if len(*sending.Transactions) == 0 {
		return out, nil
	}
	// The transaction hash is used only as an indexer lookup hint.
	hash := (*sending.Transactions)[0].Hash
	if err = read("/api/v3/adjacentTransactions", url.Values{"hash": {hash}, "direction": {"out"}}, nil, &out.Transaction); err != nil {
		return out, err
	}
	if master != "" {
		out.Dispatch, out.Parent = out.Parent, out.Transaction
		var tokenSender struct {
			Transactions *[]struct {
				Hash string `json:"hash"`
			} `json:"transactions"`
		}
		if json.Unmarshal(out.Parent, &tokenSender) != nil || tokenSender.Transactions == nil || len(*tokenSender.Transactions) > 1 {
			return out, invalid("ambiguous Jetton sender")
		}
		out.Transaction = nil
		if len(*tokenSender.Transactions) == 0 {
			return out, nil
		}
		if err = read("/api/v3/adjacentTransactions", url.Values{"hash": {(*tokenSender.Transactions)[0].Hash}, "direction": {"out"}}, nil, &out.Transaction); err != nil {
			return out, err
		}
		sender, normalizeErr := normalizeAddress(n, (*sending.Transactions)[0].Account)
		if normalizeErr != nil {
			return out, normalizeErr
		}
		if err = read("/api/v3/runGetMethod", nil, tonWalletQuery(master, sender), &out.SenderWallet); err != nil {
			return out, err
		}
		if err = read("/api/v3/runGetMethod", nil, tonWalletQuery(master, recipient), &out.Wallet); err != nil {
			return out, err
		}
	}
	var received struct {
		Transactions *[]struct {
			Block struct {
				Workchain *int   `json:"workchain"`
				Shard     string `json:"shard"`
				Seqno     int64  `json:"seqno"`
			} `json:"block_ref"`
		} `json:"transactions"`
	}
	if json.Unmarshal(out.Transaction, &received) != nil || received.Transactions == nil || len(*received.Transactions) > 1 {
		return out, invalid("ambiguous TON send recipient")
	}
	if len(*received.Transactions) == 0 {
		return out, nil
	}
	b := (*received.Transactions)[0].Block
	if b.Workchain == nil || *b.Workchain != 0 || len(b.Shard) != 16 || b.Seqno <= 0 {
		return out, invalid("invalid TON recipient block")
	}
	if _, err = hex.DecodeString(b.Shard); err != nil {
		return out, invalid("invalid TON recipient shard")
	}
	err = read("/api/v3/blocks", url.Values{"workchain": {strconv.Itoa(*b.Workchain)}, "shard": {b.Shard}, "seqno": {strconv.FormatInt(b.Seqno, 10)}, "limit": {"2"}}, nil, &out.Block)
	return out, err
}
