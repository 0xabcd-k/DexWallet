package chainreceipt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainreceipt"
	"github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	SingleUseAddress       = "single_use"
	TONMemo                = "ton_memo"
	ReceiveDeposit         = "deposit"
	ReceiveMerchantPayment = "merchant_payment"
)

var (
	ErrNoReceiveRequest = errors.New("no issued request matches this receipt")
	ErrReceiveMismatch  = errors.New("receipt differs from issued payment terms")
	ErrReceiveWindow    = errors.New("receipt outside issued collection window")
	ErrReceiveConflict  = errors.New("collection address, request or event already assigned differently")
	receiveKey          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
)

type ReceiveTerms struct {
	Key           string `json:"key"`
	Network       string `json:"network"`
	AssetID       string `json:"assetId"`
	Payer         string `json:"payer"`
	Recipient     string `json:"recipient"`
	Atoms         string `json:"atoms"`
	Purpose       string `json:"purpose"`
	BeneficiaryID int64  `json:"beneficiaryId"`
	ExpiresAt     int64  `json:"expiresAt"`
}

type EventKey struct {
	Network   string `json:"network"`
	Signature string `json:"signature"`
	Kind      string `json:"kind"`
	Index     int    `json:"index"`
}

type ReceiveRequest struct {
	ReceiveTerms
	Route     string    `json:"route"`
	Reference string    `json:"reference,omitempty"`
	OpensAt   int64     `json:"opensAt"`
	Event     *EventKey `json:"event,omitempty"`
}

type Attribution struct {
	conn         sqlx.SqlConn
	networks     []chainassets.Network
	addresses    model.ChainReceiveAddressModel
	requests     model.ChainReceiveRequestModel
	events       model.ChainTransferEventModel
	observations model.ChainObservedTransactionModel
}

func NewAttribution(conn sqlx.SqlConn, networks []chainassets.Network) *Attribution {
	return &Attribution{
		conn: conn, networks: networks,
		addresses:    model.NewChainReceiveAddressModel(conn),
		requests:     model.NewChainReceiveRequestModel(conn),
		events:       model.NewChainTransferEventModel(conn),
		observations: model.NewChainObservedTransactionModel(conn),
	}
}

func receiveOwner(network, raw string) (string, error) {
	switch network {
	case chainassets.TONTestnet:
		owner, err := tonAddress(network, raw)
		if err == nil && !strings.HasSuffix(owner, ":"+strings.Repeat("0", 64)) {
			return owner, nil
		}
	case chainassets.BaseSepolia:
		owner, err := chainassets.NormalizeBaseAddress(raw)
		if err == nil && owner != zeroEVMAddress {
			return owner, nil
		}
	case chainassets.SolanaDevnet:
		if key(raw, 32) && raw != SystemProgram {
			return raw, nil
		}
	case chainassets.TRONNile:
		if tronUserAddress(raw) {
			return raw, nil
		}
	}
	return "", ErrInvalidRequest
}

func validReceiveAtoms(network, asset, amount string) bool {
	switch network {
	case chainassets.SolanaDevnet:
		return atoms(amount)
	case chainassets.TONTestnet:
		_, err := tonNumber(amount, 120, true)
		return err == nil
	case chainassets.TRONNile:
		if asset == chainassets.TRONNile+":trx" {
			n, err := strconv.ParseInt(amount, 10, 64)
			return err == nil && n > 0 && strconv.FormatInt(n, 10) == amount
		}
	}
	return decimal256(amount)
}

func (a *Attribution) enabled(network, assetID string) bool {
	for _, n := range a.networks {
		if n.ID == network && n.Testnet && n.Enabled {
			return assetID == "" || slices.ContainsFunc(n.Assets, func(v chainassets.Asset) bool { return v.ID == assetID })
		}
	}
	return false
}

func receiveDuplicate(err error) bool {
	var e *mysql.MySQLError
	return errors.As(err, &e) && e.Number == 1062
}

// Register is for the operator's independently verified platform-owned inventory.
// A single-use address must be fresh and held permanently for the issued request.
func (a *Attribution) Register(ctx context.Context, network, owner, route string) error {
	owner, err := receiveOwner(network, owner)
	if err != nil || !a.enabled(network, "") || (route != SingleUseAddress && !(route == TONMemo && network == chainassets.TONTestnet)) {
		return ErrInvalidRequest
	}
	_, err = a.addresses.Insert(ctx, &model.ChainReceiveAddress{Network: network, Owner: owner, Route: route})
	if !receiveDuplicate(err) {
		return err
	}
	row, err := a.addresses.FindOneByNetworkOwner(ctx, network, owner)
	if err == nil && row.Route != route {
		return ErrReceiveConflict
	}
	return err
}

func (a *Attribution) Find(ctx context.Context, requestKey string) (ReceiveRequest, error) {
	if !receiveKey.MatchString(requestKey) {
		return ReceiveRequest{}, ErrInvalidRequest
	}
	row, err := a.requests.FindOneByRequestKey(ctx, requestKey)
	if err != nil {
		return ReceiveRequest{}, err
	}
	addr, err := a.addresses.FindOne(ctx, row.AddressId)
	if err != nil {
		return ReceiveRequest{}, err
	}
	out := ReceiveRequest{
		ReceiveTerms: ReceiveTerms{
			Key: row.RequestKey, Network: addr.Network, AssetID: row.AssetId,
			Payer: row.PayerOwner, Recipient: addr.Owner, Atoms: row.AmountAtoms,
			Purpose: row.Purpose, BeneficiaryID: row.BeneficiaryId, ExpiresAt: row.ExpiresAt,
		},
		Route: addr.Route, Reference: row.PaymentReference, OpensAt: row.OpensAt,
	}
	if row.MatchedEventId.Valid {
		ev, err := a.events.FindOne(ctx, row.MatchedEventId.Int64)
		if err != nil {
			return ReceiveRequest{}, err
		}
		tx, err := a.observations.FindOne(ctx, ev.TransactionId)
		if err != nil {
			return ReceiveRequest{}, err
		}
		out.Event = &EventKey{Network: tx.Network, Signature: tx.Signature, Kind: ev.EventKind, Index: int(ev.InstructionIndex)}
	}
	return out, nil
}

// Open freezes server-authorized terms before the wallet is given a destination or memo.
// At second precision, start with the first complete second after issuance.
func (a *Attribution) Open(ctx context.Context, terms ReceiveTerms) (ReceiveRequest, error) {
	var empty ReceiveRequest
	if !receiveKey.MatchString(terms.Key) || terms.BeneficiaryID <= 0 || (terms.Purpose != ReceiveDeposit && terms.Purpose != ReceiveMerchantPayment) || !validReceiveAtoms(terms.Network, terms.AssetID, terms.Atoms) || terms.ExpiresAt <= 0 {
		return empty, ErrInvalidRequest
	}
	var err error
	terms.Recipient, err = receiveOwner(terms.Network, terms.Recipient)
	if err != nil {
		return empty, err
	}
	terms.Payer, err = receiveOwner(terms.Network, terms.Payer)
	if err != nil || terms.Payer == terms.Recipient {
		return empty, ErrInvalidRequest
	}
	existing := func() (ReceiveRequest, error) {
		found, err := a.Find(ctx, terms.Key)
		if err == nil && found.ReceiveTerms != terms {
			return empty, ErrReceiveConflict
		}
		return found, err
	}
	if old, err := existing(); !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	if terms.AssetID == "" || !a.enabled(terms.Network, terms.AssetID) {
		return empty, ErrInvalidRequest
	}
	addr, err := a.addresses.FindOneByNetworkOwner(ctx, terms.Network, terms.Recipient)
	if err != nil {
		return empty, fmt.Errorf("registered platform address required: %w", err)
	}
	if _, err = a.addresses.FindOneByNetworkOwner(ctx, terms.Network, terms.Payer); !errors.Is(err, model.ErrNotFound) {
		if err != nil {
			return empty, err
		}
		return empty, fmt.Errorf("%w: platform source", ErrInvalidRequest)
	}
	ref := ""
	if addr.Route == TONMemo {
		var token [16]byte
		if _, err = rand.Read(token[:]); err != nil {
			return empty, err
		}
		ref = receiveMemoPrefix + hex.EncodeToString(token[:])
	}
	res, err := a.conn.ExecCtx(ctx, `INSERT INTO chain_receive_request
		(request_key,address_id,payment_reference,purpose,beneficiary_id,payer_owner,asset_id,amount_atoms,opens_at,expires_at)
		SELECT ?,?,?,?,?,?,?,?,UNIX_TIMESTAMP()+1,? WHERE UNIX_TIMESTAMP()+1<?`, terms.Key, addr.Id, ref, terms.Purpose, terms.BeneficiaryID, terms.Payer, terms.AssetID, terms.Atoms, terms.ExpiresAt, terms.ExpiresAt)
	if receiveDuplicate(err) {
		old, lookupErr := existing()
		if errors.Is(lookupErr, model.ErrNotFound) {
			return empty, ErrReceiveConflict
		}
		return old, lookupErr
	}
	if err != nil {
		return empty, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return empty, err
	}
	if n == 0 {
		if old, e := existing(); !errors.Is(e, model.ErrNotFound) {
			return old, e
		}
		return empty, ErrReceiveWindow
	}
	return existing()
}

func (k EventKey) normalized() (EventKey, error) {
	if k.Index < 0 || k.Index > math.MaxInt32 {
		return k, ErrInvalidRequest
	}
	switch k.Network {
	case chainassets.SolanaDevnet:
		if k.Kind == SolanaInstruction && key(k.Signature, 64) {
			return k, nil
		}
	case chainassets.BaseSepolia:
		if hexData(k.Signature, 32) && ((k.Kind == EVMValue && k.Index == 0) || k.Kind == EVMLog || (k.Kind == EVMCall && k.Index > 0)) {
			k.Signature = strings.ToLower(k.Signature)
			return k, nil
		}
	case chainassets.TRONNile:
		if tronHex(k.Signature, 32) && ((k.Kind == TRONTransfer && k.Index == 0) || k.Kind == TRONLog) {
			k.Signature = strings.ToLower(k.Signature)
			return k, nil
		}
	case chainassets.TONTestnet:
		h, err := tonHash(k.Signature)
		if err == nil && k.Index == 0 && (k.Kind == TONIncoming || k.Kind == TONJetton) {
			k.Signature = h
			return k, nil
		}
	}
	return k, ErrInvalidRequest
}

// Match accepts only the location of a previously verified event. The stored owner and
// the event's own message determine the request. The caller cannot select a beneficiary.
func (a *Attribution) Match(ctx context.Context, hint EventKey) (ReceiveRequest, error) {
	var empty ReceiveRequest
	k, err := hint.normalized()
	if err != nil {
		return empty, err
	}
	tx, err := a.observations.FindOneByNetworkSignature(ctx, k.Network, k.Signature)
	if err != nil {
		return empty, err
	}
	ev, err := a.events.FindOneByTransactionIdEventKindInstructionIndex(ctx, tx.Id, k.Kind, int64(k.Index))
	if err != nil {
		return empty, err
	}
	addr, err := a.addresses.FindOneByNetworkOwner(ctx, k.Network, ev.RecipientOwner)
	if errors.Is(err, model.ErrNotFound) {
		return empty, ErrNoReceiveRequest
	}
	if err != nil {
		return empty, err
	}
	when, ref, err := receiveEvidence(k, tx.EvidenceJson, addr.Route)
	if err != nil {
		return empty, err
	}
	row, err := a.requests.FindOneByAddressIdPaymentReference(ctx, addr.Id, ref)
	if errors.Is(err, model.ErrNotFound) {
		return empty, ErrNoReceiveRequest
	}
	if err != nil {
		return empty, err
	}
	if row.PayerOwner != ev.SourceOwner || row.AssetId != ev.AssetId || row.AmountAtoms != ev.AmountAtoms {
		return empty, ErrReceiveMismatch
	}
	if when < row.OpensAt || when >= row.ExpiresAt {
		return empty, ErrReceiveWindow
	}
	if !row.MatchedEventId.Valid {
		// A request can accept one event and the unique index reserves each event globally.
		_, err = a.conn.ExecCtx(ctx, `UPDATE chain_receive_request SET matched_event_id=?
			WHERE id=? AND matched_event_id IS NULL AND NOT EXISTS (
				SELECT 1 FROM chain_receive_address WHERE network=? AND owner=?)`, ev.Id, row.Id, k.Network, ev.SourceOwner)
		if receiveDuplicate(err) {
			return empty, ErrReceiveConflict
		}
		if err != nil {
			return empty, err
		}
	}
	found, err := a.Find(ctx, row.RequestKey)
	if err != nil {
		return empty, err
	}
	if found.Event == nil {
		return empty, fmt.Errorf("%w: platform source withheld", ErrReceiveMismatch)
	}
	if *found.Event != k {
		return empty, ErrReceiveConflict
	}
	return found, nil
}
