package chainreceipt

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const TONIncoming = "ton_incoming"
const TONJetton = "ton_jetton"
const tonNativeProgram = "ton"

type TONRequest struct {
	Network   string `json:"network"`
	Hash      string `json:"hash"` // Recipient transaction, not a sender/external message.
	AssetID   string `json:"assetId"`
	Recipient string `json:"recipient"`
	Atoms     string `json:"atoms"`
}
type TONSource interface {
	Catalog() []chainassets.Network
	ReadTONEvidence(context.Context, string, string, string, string) (chainassets.TONEvidence, error)
}
type TONVerifier struct{ source TONSource }

func NewTONVerifier(source TONSource) *TONVerifier { return &TONVerifier{source} }

type TONService struct {
	verifier *TONVerifier
	store    *Service
}

func NewTON(conn sqlx.SqlConn, tx Transactor, source TONSource) *TONService {
	return &TONService{NewTONVerifier(source), &Service{conn: conn, tx: tx}}
}
func (s *TONService) Record(ctx context.Context, r TONRequest) (Stored, error) {
	v, err := s.verifier.Verify(ctx, r)
	if err != nil {
		return Stored{}, err
	}
	return s.store.record(ctx, v)
}
func (s *TONService) Find(ctx context.Context, network, hash, kind string) (Receipt, error) {
	h, err := tonHash(hash)
	if err != nil || (network != chainassets.TONMainnet && network != chainassets.TONTestnet) || (kind != TONIncoming && kind != TONJetton) {
		return Receipt{}, ErrInvalidRequest
	}
	return s.store.findEvent(ctx, network, h, kind, 0)
}
func tonHash(raw string) (string, error) {
	if len(raw) == 64 {
		if b, err := hex.DecodeString(raw); err == nil {
			return hex.EncodeToString(b), nil
		}
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(b) != 32 {
		return "", bad("invalid TON hash")
	}
	return hex.EncodeToString(b), nil
}
func tonAddress(network, raw string) (string, error) {
	if len(raw) > 128 || strings.TrimSpace(raw) != raw {
		return "", bad("invalid TON address")
	}
	var a *address.Address
	var err error
	if strings.Contains(raw, ":") {
		a, err = address.ParseRawAddr(raw)
	} else {
		a, err = address.ParseAddr(raw)
		if err == nil && network == chainassets.TONMainnet && a.IsTestnetOnly() {
			return "", bad("test-only TON address on mainnet")
		}
	}
	if err != nil || a.Type() != address.StdAddress || (a.Workchain() != 0 && a.Workchain() != -1) {
		return "", bad("invalid TON address")
	}
	return a.StringRaw(), nil
}
func tonNumber(raw string, bits int, positive bool) (*big.Int, error) {
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok || n.Sign() < 0 || (positive && n.Sign() == 0) || n.BitLen() > bits || n.String() != raw {
		return nil, bad("invalid TON integer quantity")
	}
	return n, nil
}
func (v *TONVerifier) Verify(ctx context.Context, r TONRequest) (Receipt, error) {
	hash, err := tonHash(r.Hash)
	if err != nil {
		return Receipt{}, ErrInvalidRequest
	}
	r.Hash = hash
	owner, err := tonAddress(r.Network, r.Recipient)
	if err != nil {
		return Receipt{}, ErrInvalidRequest
	}
	r.Recipient = owner
	if _, err = tonNumber(r.Atoms, 120, true); err != nil {
		return Receipt{}, ErrInvalidRequest
	}
	var asset chainassets.Asset
	for _, n := range v.source.Catalog() {
		if n.ID == r.Network && n.Family == chainassets.TON {
			for _, a := range n.Assets {
				if a.ID == r.AssetID {
					asset = a
					break
				}
			}
		}
	}
	if asset.ID == "" {
		return Receipt{}, ErrInvalidRequest
	}
	if asset.Contract != "" {
		asset.Contract, err = tonAddress(r.Network, asset.Contract)
		if err != nil {
			return Receipt{}, ErrInvalidRequest
		}
	}
	proof, err := v.source.ReadTONEvidence(ctx, r.Network, r.Hash, asset.Contract, r.Recipient)
	if err != nil {
		return Receipt{}, fmt.Errorf("read TON receipt %s/%s: %w", r.Network, r.Hash, err)
	}
	return verifyTON(r, asset, proof)
}

type tonBlockRef struct {
	Workchain *int   `json:"workchain"`
	Shard     string `json:"shard"`
	Seqno     int64  `json:"seqno"`
}

func (b tonBlockRef) valid() bool {
	_, err := hex.DecodeString(b.Shard)
	return b.Workchain != nil && (*b.Workchain == 0 || *b.Workchain == -1) && len(b.Shard) == 16 && err == nil && b.Seqno > 0
}
func (b tonBlockRef) equal(c tonBlockRef) bool {
	return b.valid() && c.valid() && *b.Workchain == *c.Workchain && b.Shard == c.Shard && b.Seqno == c.Seqno
}

type tonContent struct {
	Hash string `json:"hash"`
	Body string `json:"body"`
}
type tonMessage struct {
	Hash        string            `json:"hash"`
	Source      string            `json:"source"`
	Destination string            `json:"destination"`
	Value       string            `json:"value"`
	Extra       map[string]string `json:"value_extra_currencies"`
	ForwardFee  string            `json:"fwd_fee"`
	IHRFee      string            `json:"ihr_fee"`
	CreatedLT   string            `json:"created_lt"`
	Bounce      *bool             `json:"bounce"`
	Bounced     *bool             `json:"bounced"`
	IHRDisabled *bool             `json:"ihr_disabled"`
	Content     *tonContent       `json:"message_content"`
	Init        *tonContent       `json:"init_state"`
}
type tonDescription struct {
	Type      string `json:"type"`
	Aborted   *bool  `json:"aborted"`
	Destroyed *bool  `json:"destroyed"`
	Credit    *struct {
		Credit string            `json:"credit"`
		Extra  map[string]string `json:"credit_extra_currencies"`
	} `json:"credit_ph"`
	Compute *struct {
		Skipped  *bool  `json:"skipped"`
		Success  *bool  `json:"success"`
		ExitCode *int   `json:"exit_code"`
		Reason   string `json:"reason"`
	} `json:"compute_ph"`
	Action *struct {
		Success  *bool `json:"success"`
		Valid    *bool `json:"valid"`
		NoFunds  *bool `json:"no_funds"`
		Result   *int  `json:"result_code"`
		Skipped  *int  `json:"skipped_actions"`
		Messages *int  `json:"msgs_created"`
	} `json:"action"`
	Bounce json.RawMessage `json:"bounce,omitempty"`
}
type tonTransaction struct {
	Account     string            `json:"account"`
	Hash        string            `json:"hash"`
	LT          string            `json:"lt"`
	Now         int64             `json:"now"`
	MCSeqno     int64             `json:"mc_block_seqno"`
	TraceID     string            `json:"trace_id"`
	EndStatus   string            `json:"end_status"`
	TotalFees   string            `json:"total_fees"`
	ExtraFees   map[string]string `json:"total_fees_extra_currencies"`
	Description *tonDescription   `json:"description"`
	Block       tonBlockRef       `json:"block_ref"`
	In          *tonMessage       `json:"in_msg"`
	Out         *[]tonMessage     `json:"out_msgs"`
	Emulated    *bool             `json:"emulated"`
	Finality    string            `json:"finality"`
}

func tonOneTransaction(raw json.RawMessage) (tonTransaction, error) {
	var r struct {
		Transactions *[]tonTransaction `json:"transactions"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Transactions == nil || len(*r.Transactions) > 1 {
		return tonTransaction{}, bad("missing/ambiguous TON transactions")
	}
	if len(*r.Transactions) == 0 {
		return tonTransaction{}, ErrUnconfirmed
	}
	return (*r.Transactions)[0], nil
}
func isTrue(p *bool) bool  { return p != nil && *p }
func isFalse(p *bool) bool { return p != nil && !*p }
func tonCell(raw string) (*cell.Cell, error) {
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, bad("invalid TON BoC encoding")
	}
	roots, err := cell.FromBOCMultiRoot(b)
	if err != nil || len(roots) != 1 || roots[0].GetType() != cell.OrdinaryCellType {
		return nil, bad("invalid TON ordinary single-root BoC")
	}
	return roots[0], nil
}
func tonBody(m *tonMessage) (*cell.Cell, error) {
	if m == nil || m.Content == nil {
		return nil, bad("missing TON message body")
	}
	c, err := tonCell(m.Content.Body)
	if err != nil {
		return nil, err
	}
	hash, err := tonHash(m.Content.Hash)
	if err != nil || hash != hex.EncodeToString(c.Hash()) {
		return nil, bad("TON body hash mismatch")
	}
	return c, nil
}
func normalizeTONTransaction(n string, t *tonTransaction, allowExternalInput bool) error {
	var err error
	for _, p := range []*string{&t.Hash, &t.TraceID} {
		*p, err = tonHash(*p)
		if err != nil {
			return err
		}
	}
	t.Account, err = tonAddress(n, t.Account)
	if err != nil {
		return err
	}
	if t.Now <= 0 || t.MCSeqno <= 0 || !t.Block.valid() || t.Out == nil || t.Description == nil || t.In == nil {
		return bad("missing TON transaction facts")
	}
	if _, err = tonNumber(t.LT, 64, true); err != nil {
		return err
	}
	if _, err = tonNumber(t.TotalFees, 120, false); err != nil {
		return err
	}
	if len(t.ExtraFees) != 0 {
		return ErrUnsupported
	}
	if !isFalse(t.Emulated) || t.Finality != Finalized {
		return ErrUnconfirmed
	}
	for _, m := range append([]*tonMessage{t.In}, tonOutgoing(t)...) {
		m.Hash, err = tonHash(m.Hash)
		if err != nil {
			return err
		}
		if allowExternalInput && m == t.In && m.Source == "" {
			m.Destination, err = tonAddress(n, m.Destination)
			if err != nil || m.Value != "" || m.CreatedLT != "" || len(m.Extra) != 0 {
				return bad("invalid TON external input")
			}
			c, err := tonBody(m)
			if err != nil {
				return err
			}
			m.Content.Hash = hex.EncodeToString(c.Hash())
			m.Content.Body = base64.StdEncoding.EncodeToString(c.ToBOC())
			continue
		}
		for _, p := range []*string{&m.Source, &m.Destination} {
			*p, err = tonAddress(n, *p)
			if err != nil {
				return err
			}
		}
		if m.Bounce == nil || m.Bounced == nil || !isTrue(m.IHRDisabled) || len(m.Extra) != 0 {
			return bad("unsupported TON message flags/currencies")
		}
		for _, v := range []string{m.Value, m.ForwardFee, m.IHRFee} {
			if _, err = tonNumber(v, 120, false); err != nil {
				return err
			}
		}
		if m.IHRFee != "0" {
			return ErrUnsupported
		}
		if _, err = tonNumber(m.CreatedLT, 64, true); err != nil {
			return err
		}
		c, err := tonBody(m)
		if err != nil {
			return err
		}
		m.Content.Hash = hex.EncodeToString(c.Hash())
		m.Content.Body = base64.StdEncoding.EncodeToString(c.ToBOC())
	}
	if t.In.Destination != t.Account {
		return bad("TON incoming destination differs from account")
	}
	return nil
}
func tonOutgoing(t *tonTransaction) []*tonMessage {
	out := make([]*tonMessage, len(*t.Out))
	for i := range *t.Out {
		out[i] = &(*t.Out)[i]
	}
	return out
}
func tonSuccessful(t tonTransaction) error {
	d := t.Description
	if d.Type != "ord" || d.Aborted == nil || d.Destroyed == nil || d.Compute == nil || d.Compute.Skipped == nil {
		return bad("missing TON execution facts")
	}
	if *d.Aborted || *d.Destroyed || !nullOrAbsent(d.Bounce) {
		return ErrExecutionFailed
	}
	if *d.Compute.Skipped || !isTrue(d.Compute.Success) || d.Compute.ExitCode == nil || (*d.Compute.ExitCode != 0 && *d.Compute.ExitCode != 1) {
		return ErrExecutionFailed
	}
	a := d.Action
	if a == nil || !isTrue(a.Success) || !isTrue(a.Valid) || !isFalse(a.NoFunds) || a.Result == nil || *a.Result != 0 || a.Skipped == nil || *a.Skipped != 0 || a.Messages == nil || *a.Messages != len(*t.Out) {
		return ErrExecutionFailed
	}
	return nil
}
func nullOrAbsent(raw json.RawMessage) bool { return len(raw) == 0 || null(raw) }
func tonNativeCredit(t tonTransaction) error {
	d := t.Description
	if len(*t.Out) != 0 || t.In.Init != nil {
		return ErrUnsupported
	}
	if d.Credit == nil || d.Credit.Credit != t.In.Value || len(d.Credit.Extra) != 0 {
		return bad("TON credit differs from incoming value")
	}
	if d.Compute != nil && isTrue(d.Compute.Skipped) {
		if d.Type != "ord" || !isTrue(d.Aborted) || !isFalse(d.Destroyed) || !nullOrAbsent(d.Bounce) || !isFalse(t.In.Bounce) || d.Action != nil {
			return ErrExecutionFailed
		}
		switch d.Compute.Reason {
		case "no_state":
			if t.EndStatus != "uninit" {
				return ErrExecutionFailed
			}
		case "no_gas":
			if t.EndStatus != "active" {
				return ErrExecutionFailed
			}
		default:
			return ErrUnsupported
		}
		return nil
	}
	return tonSuccessful(t)
}

type tonInternalTransfer struct {
	_          tlb.Magic        `tlb:"#178d4519"`
	QueryID    uint64           `tlb:"## 64"`
	Amount     tlb.Coins        `tlb:"."`
	From       *address.Address `tlb:"addr"`
	Response   *address.Address `tlb:"addr"`
	ForwardTON tlb.Coins        `tlb:"."`
	Payload    *cell.Cell       `tlb:"either . ^"`
}

func tonPayload(c *cell.Cell, v any) error {
	s := c.BeginParse()
	if tlb.LoadFromCell(v, s) != nil || s.BitsLeft() != 0 || s.RefsNum() != 0 {
		return bad("invalid TON transfer payload")
	}
	return nil
}
func tonParsedAddress(n string, a *address.Address) (string, error) {
	if a == nil || a.Type() != address.StdAddress {
		return "", bad("missing standard TON payload address")
	}
	return tonAddress(n, a.StringRaw())
}
func tonDerivedWallet(raw json.RawMessage, network string) (string, error) {
	var r struct {
		ExitCode *int `json:"exit_code"`
		Stack    []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"stack"`
	}
	if json.Unmarshal(raw, &r) != nil || r.ExitCode == nil || *r.ExitCode != 0 || len(r.Stack) != 1 || (r.Stack[0].Type != "cell" && r.Stack[0].Type != "slice") {
		return "", bad("invalid get_wallet_address result")
	}
	c, err := tonCell(r.Stack[0].Value)
	if err != nil {
		return "", err
	}
	s := c.BeginParse()
	a, err := s.LoadAddr()
	if err != nil || s.BitsLeft() != 0 || s.RefsNum() != 0 {
		return "", bad("invalid derived Jetton wallet")
	}
	return tonParsedAddress(network, a)
}
func verifyTON(r TONRequest, asset chainassets.Asset, e chainassets.TONEvidence) (Receipt, error) {
	var out Receipt
	var head struct {
		OK     bool `json:"ok"`
		Result struct {
			Init struct {
				Root      string `json:"root_hash"`
				Seqno     int64  `json:"seqno"`
				Workchain *int   `json:"workchain"`
			} `json:"init"`
			Last struct {
				Seqno     int64  `json:"seqno"`
				Workchain *int   `json:"workchain"`
				Root      string `json:"root_hash"`
			} `json:"last"`
		} `json:"result"`
	}
	want := chainassets.TONMainnetZeroState
	if r.Network == chainassets.TONTestnet {
		want = chainassets.TONTestnetZeroState
	}
	if json.Unmarshal(e.Masterchain, &head) != nil || !head.OK || head.Result.Init.Root != want || head.Result.Init.Seqno != 0 || head.Result.Init.Workchain == nil || *head.Result.Init.Workchain != -1 || head.Result.Last.Workchain == nil || *head.Result.Last.Workchain != -1 {
		return out, bad("wrong TON network/head")
	}
	if _, err := tonHash(head.Result.Last.Root); err != nil {
		return out, err
	}
	tx, err := tonOneTransaction(e.Transaction)
	if err != nil {
		return out, err
	}
	parent, err := tonOneTransaction(e.Parent)
	if err != nil {
		return out, err
	}
	if err = normalizeTONTransaction(r.Network, &tx, false); err != nil {
		return out, err
	}
	if err = normalizeTONTransaction(r.Network, &parent, true); err != nil {
		return out, err
	}
	if tx.Hash != r.Hash || tx.Hash == parent.Hash || tx.TraceID != parent.TraceID || parent.Account != tx.In.Source || isTrue(tx.In.Bounced) {
		return out, bad("wrong TON transaction/message identity")
	}
	if tx.MCSeqno > head.Result.Last.Seqno || parent.MCSeqno > head.Result.Last.Seqno {
		return out, ErrUnconfirmed
	}
	if parent.MCSeqno > tx.MCSeqno {
		return out, bad("TON parent is later than recipient")
	}
	if err = tonSuccessful(parent); err != nil {
		return out, err
	}
	txLT, _ := tonNumber(tx.LT, 64, true)
	parentLT, _ := tonNumber(parent.LT, 64, true)
	created, _ := tonNumber(tx.In.CreatedLT, 64, true)
	if parentLT.Cmp(created) >= 0 || created.Cmp(txLT) >= 0 {
		return out, bad("invalid TON message LT ordering")
	}
	matches := 0
	for _, m := range *parent.Out {
		if m.Hash == tx.In.Hash {
			matches++
			a, _ := json.Marshal(m)
			b, _ := json.Marshal(tx.In)
			if string(a) != string(b) {
				return out, bad("TON sent/received message differs")
			}
		}
	}
	if matches != 1 {
		return out, bad("TON message absent or duplicated at sender")
	}
	var blocks struct {
		Blocks *[]struct {
			tonBlockRef
			Root     string      `json:"root_hash"`
			GlobalID *int        `json:"global_id"`
			Time     string      `json:"gen_utime"`
			StartLT  string      `json:"start_lt"`
			EndLT    string      `json:"end_lt"`
			Master   tonBlockRef `json:"masterchain_block_ref"`
		} `json:"blocks"`
	}
	if json.Unmarshal(e.Block, &blocks) != nil || blocks.Blocks == nil || len(*blocks.Blocks) != 1 {
		return out, bad("missing/ambiguous TON block")
	}
	block := (*blocks.Blocks)[0]
	networkID := -239
	if r.Network == chainassets.TONTestnet {
		networkID = -3
	}
	if !block.tonBlockRef.equal(tx.Block) || block.GlobalID == nil || *block.GlobalID != networkID || !block.Master.valid() || *block.Master.Workchain != -1 || block.Master.Seqno != tx.MCSeqno || block.Time != strconv.FormatInt(tx.Now, 10) {
		return out, bad("TON shard/masterchain block differs")
	}
	root, err := tonHash(block.Root)
	if err != nil {
		return out, err
	}
	start, err := tonNumber(block.StartLT, 64, true)
	if err != nil {
		return out, err
	}
	end, err := tonNumber(block.EndLT, 64, true)
	if err != nil || start.Cmp(txLT) > 0 || end.Cmp(txLT) <= 0 {
		return out, bad("TON LT outside block")
	}
	body, err := tonBody(tx.In)
	if err != nil {
		return out, err
	}
	event := Event{AssetID: asset.ID, Decimals: asset.Decimals, SourceAccount: tx.In.Source, RecipientAccount: tx.Account, RecipientOwner: r.Recipient}
	derived := ""
	if asset.Origin == chainassets.Native {
		if err = tonNativeCredit(tx); err != nil {
			return out, err
		}
		s := body.BeginParse()
		if s.BitsLeft() != 0 || s.RefsNum() != 0 {
			op, err := s.LoadUInt(32)
			if err != nil || op != 0 {
				return out, ErrUnsupported
			}
			comment, err := s.LoadStringSnake()
			if err != nil || !utf8.ValidString(comment) {
				return out, bad("invalid TON comment")
			}
		}
		event.Kind = TONIncoming
		event.ProgramID = tonNativeProgram
		event.SourceOwner = tx.In.Source
		event.Atoms = tx.In.Value
		if tx.Account != r.Recipient {
			return out, bad("wrong TON recipient")
		}
	} else {
		if err = tonSuccessful(tx); err != nil {
			return out, err
		}
		derived, err = tonDerivedWallet(e.Wallet, r.Network)
		if err != nil || derived != tx.Account {
			return out, bad("recipient Jetton wallet not derived from allowed master")
		}
		var received tonInternalTransfer
		if err = tonPayload(body, &received); err != nil {
			return out, err
		}
		from, err := tonParsedAddress(r.Network, received.From)
		if err != nil {
			return out, err
		}
		sentBody, err := tonBody(parent.In)
		if err != nil {
			return out, err
		}
		var sent jetton.TransferPayload
		if err = tonPayload(sentBody, &sent); err != nil {
			return out, err
		}
		destination, err := tonParsedAddress(r.Network, sent.Destination)
		if err != nil {
			return out, err
		}
		if destination != r.Recipient || parent.In.Source != from || sent.QueryID != received.QueryID || sent.Amount.Nano().Cmp(received.Amount.Nano()) != 0 || sent.ForwardTONAmount.Nano().Cmp(received.ForwardTON.Nano()) != 0 {
			return out, bad("Jetton transfer/internal_transfer disagree")
		}
		event.Kind = TONJetton
		event.Mint = asset.Contract
		event.ProgramID = asset.Contract
		event.SourceOwner = from
		event.Atoms = received.Amount.Nano().String()
	}
	if event.Atoms != r.Atoms || event.SourceOwner == event.RecipientOwner {
		return out, bad("TON amount differs or self transfer")
	}
	fee, _ := tonNumber(tx.TotalFees, 120, false)
	for _, m := range *tx.Out {
		n, _ := tonNumber(m.ForwardFee, 120, false)
		fee.Add(fee, n)
	}
	if fee.BitLen() > 256 {
		return out, bad("TON fees exceed uint256")
	}
	// Store the receive transaction's cost, not the whole payment trace's cost.
	normalized, _ := json.Marshal(struct {
		Transaction, Parent tonTransaction
		BlockRoot, Wallet   string
	}{tx, parent, root, derived})
	digest := sha256.Sum256(normalized)
	out = Receipt{Transaction: Transaction{Network: r.Network, Signature: r.Hash, Slot: tx.MCSeqno, FeePayer: tx.Account, FeeAtoms: fee.String(), BlockHash: root, Fingerprint: hex.EncodeToString(digest[:])}, Event: event, evidence: e}
	return out, nil
}
