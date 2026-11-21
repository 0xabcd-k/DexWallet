//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/mr-tron/base58"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type tronSendNode struct {
	head   tronnative.Head
	seen   bool
	seeErr error
	submit func(tronnative.Signed) error
}

func (n *tronSendNode) NileHead(context.Context) (tronnative.Head, error) { return n.head, nil }
func (n *tronSendNode) NileTransactionSeen(context.Context, string) (bool, error) {
	return n.seen, n.seeErr
}
func (n *tronSendNode) SubmitNileTRX(_ context.Context, s tronnative.Signed) error {
	return n.submit(s)
}

type tronUSDTNode struct {
	*tronSendNode
	contract    string
	submitToken func(tronnative.Signed) error
}

func (n *tronUSDTNode) NileUSDTContract() string { return n.contract }
func (n *tronUSDTNode) SubmitNileUSDT(_ context.Context, s tronnative.Signed) error {
	return n.submitToken(s)
}

type tronPayoutSource struct {
	*tronReceiptSource
	err error
}

func (s *tronPayoutSource) ReadTRONEvidence(ctx context.Context, network, hash string) (chainassets.TRONEvidence, error) {
	if s.err != nil {
		return chainassets.TRONEvidence{}, s.err
	}
	return s.tronReceiptSource.ReadTRONEvidence(ctx, network, hash)
}

func TestTRONPayoutOriginalBytesAndExpiry(t *testing.T) {
	checkTRONPayoutReplay(t, "")
}

func TestTRONUSDTPayoutReplay(t *testing.T) {
	checkTRONPayoutReplay(t, "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf")
}

func checkTRONPayoutReplay(t *testing.T, token string) {
	ctx := context.Background()
	label := "nile_payout_retry"
	if token != "" {
		label = "nile_usdt_retry"
	}
	db, _ := userDatabase(t, label)
	req, src := tronReceiptFixture(t, "tron-trx")
	absent := chainassets.TRONEvidence{Genesis: src.proof.Genesis, Transaction: json.RawMessage("{}"), Info: json.RawMessage("{}")}
	src.proof = absent
	proof := &tronPayoutSource{tronReceiptSource: src}
	conn := sqlx.NewSqlConnFromDB(db)
	receipt := chainreceipt.NewTRON(conn, transaction.New(db), proof)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	payload := append([]byte{0x41}, crypto.PubkeyToAddress(key.PublicKey).Bytes()...)
	h := sha256.Sum256(payload)
	checksum := sha256.Sum256(h[:])
	sender := base58.Encode(append(payload, checksum[:4]...))
	node := &tronSendNode{head: tronnative.Head{ID: "0000000000000001d698d4192c56cb6be724a558448e2684802de4d6cd8690dc", Height: 1, Timestamp: time.Now().UnixMilli()}}
	var rpc chainsend.TRONNode = node
	tokenNode := &tronUSDTNode{tronSendNode: node, contract: token}
	if token != "" {
		rpc = tokenNode
	}
	open := func() *chainsend.TRON {
		s, err := chainsend.NewTRON(conn, sender, rpc, receipt)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	prepare := func(id string, amount int64) (*model.TronPayout, error) {
		if token != "" {
			return s.PrepareUSDT(ctx, id, req.Recipient, token, strconv.FormatInt(amount, 10), 3000000)
		}
		return s.Prepare(ctx, id, req.Recipient, amount)
	}
	const id = "nile-retry"
	var group sync.WaitGroup
	for range 6 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := prepare(id, 1000); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	p, err := s.Find(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prepare(id, 1001); !errors.Is(err, chainsend.ErrConflict) {
		t.Fatal("changed intent", err)
	}
	if _, err = prepare("another-key", 1000); !errors.Is(err, chainsend.ErrConflict) {
		t.Fatal("duplicate raw tx", err)
	}
	if _, err = s.Step(ctx, id); !errors.Is(err, chainsend.ErrInvalidSignature) {
		t.Fatal(err)
	}
	digest, _ := hex.DecodeString(p.TxId)
	sig, err := crypto.Sign(digest, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Bind(ctx, id, hex.EncodeToString(sig)); err != nil {
		t.Fatal(err)
	}
	proof.err = io.EOF
	if _, err = s.Step(ctx, id); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	proof.err = nil
	src.proof.Genesis = json.RawMessage("{}")
	if _, err = s.Step(ctx, id); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
		t.Fatal("unverified absence", err)
	}
	src.proof = absent
	src.proof.Transaction = json.RawMessage(`{"txID":"partially visible"}`)
	if _, err = s.Step(ctx, id); !errors.Is(err, chainreceipt.ErrUnconfirmed) {
		t.Fatal("partial receipt", err)
	}
	src.proof = absent
	node.seeErr = io.ErrUnexpectedEOF
	if _, err = s.Step(ctx, id); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	node.seeErr = nil
	node.seen = true
	if _, err = s.Step(ctx, id); !errors.Is(err, chainreceipt.ErrUnconfirmed) {
		t.Fatal(err)
	}
	node.seen = false
	calls := 0
	var first string
	submit := func(signed tronnative.Signed) error {
		if signed.TokenContract() != token {
			t.Fatal("wrong broadcast envelope")
		}
		calls++
		row, err := s.Find(ctx, id)
		if err != nil || row.TxId != signed.ID() || row.Attempts != int64(calls) || row.Signature != hex.EncodeToString(sig) {
			t.Fatal("broadcast before commit", err)
		}
		if calls == 1 {
			first = signed.Hex()
			return io.ErrUnexpectedEOF
		}
		if signed.Hex() != first {
			t.Fatal("replay changed bytes")
		}
		return nil
	}
	if token == "" {
		node.submit = submit
	} else {
		tokenNode.submitToken = submit
		node.submit = func(tronnative.Signed) error { t.Fatal("USDT sent as native TRX"); return nil }
		tokenNode.contract = req.Recipient
		if row, err := s.Step(ctx, id); !errors.Is(err, chainsend.ErrInvalidRequest) || row.Attempts != 0 {
			t.Fatal("changed contract admitted for replay", row, err)
		}
		tokenNode.contract = token
	}
	if _, err = s.Step(ctx, id); !errors.Is(err, chainsend.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	s = open()
	row, err := s.Step(ctx, id)
	if err != nil || row.State != chainsend.Submitted || calls != 2 {
		t.Fatal(row, err)
	}
	if _, err = s.Step(ctx, id); !errors.Is(err, chainreceipt.ErrUnconfirmed) || calls != 2 {
		t.Fatal("acknowledged transfer replayed", err)
	}
	node.head.Timestamp = p.ExpirationMs
	if _, err = s.Step(ctx, id); !errors.Is(err, chainsend.ErrTRONExpired) || calls != 2 {
		t.Fatal("expired transfer replayed", err)
	}
	again, err := prepare(id, 1000)
	if err != nil || again.TxId != p.TxId || again.ExpirationMs != p.ExpirationMs || again.Attempts != 2 {
		t.Fatal("expired request replaced", again, err)
	}
}

func TestTRONPayoutSolidReceiptAndRestart(t *testing.T) {
	checkTRONSolidRestart(t, "trx")
}
func TestTRONUSDTPayoutSolidReceiptAndRestart(t *testing.T) {
	for _, name := range []string{"usdt", "usdt-fixture"} {
		t.Run(name, func(t *testing.T) { checkTRONSolidRestart(t, name) })
	}
}
func checkTRONSolidRestart(t *testing.T, name string) {
	ctx := context.Background()
	db, _ := userDatabase(t, "nile_solid_"+strings.ReplaceAll(name, "-", "_"))
	req, src := tronReceiptFixture(t, "tron-payout-"+name)
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/tron-payout-" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		Intent  model.TronPayout
		Receipt chainreceipt.Receipt
	}
	if err = json.Unmarshal(raw, &capture); err != nil {
		t.Fatal(err)
	}
	p := capture.Intent
	conn := sqlx.NewSqlConnFromDB(db)
	proof := &tronPayoutSource{tronReceiptSource: src}
	receipts := chainreceipt.NewTRON(conn, transaction.New(db), proof)
	node := &tronSendNode{head: tronnative.Head{ID: p.RefBlockId, Height: p.RefHeight, Timestamp: p.TimestampMs}, submit: func(tronnative.Signed) error { t.Fatal("historical payout broadcast"); return nil }}
	tokenNode := &tronUSDTNode{tronSendNode: node, contract: p.TokenContract, submitToken: node.submit}
	open := func() *chainsend.TRON {
		s, err := chainsend.NewTRON(conn, p.Sender, tokenNode, receipts)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	var prepared *model.TronPayout
	if p.TokenContract == "" {
		prepared, err = s.Prepare(ctx, p.RequestKey, p.Recipient, p.Sun)
	} else {
		tokenNode.contract = ""
		if _, err = s.PrepareUSDT(ctx, p.RequestKey, p.Recipient, p.TokenContract, p.TokenAtoms, p.FeeLimitSun); !errors.Is(err, chainsend.ErrInvalidRequest) {
			t.Fatal("unconfigured token prepared", err)
		}
		tokenNode.contract = p.TokenContract
		prepared, err = s.PrepareUSDT(ctx, p.RequestKey, p.Recipient, p.TokenContract, p.TokenAtoms, p.FeeLimitSun)
		if _, e := s.PrepareUSDT(ctx, p.RequestKey, p.Recipient, p.TokenContract, p.TokenAtoms, p.FeeLimitSun+1); !errors.Is(e, chainsend.ErrConflict) {
			t.Fatal("fee limit changed", e)
		}
		if _, e := s.Prepare(ctx, p.RequestKey, p.Recipient, 1000); !errors.Is(e, chainsend.ErrConflict) {
			t.Fatal("native/USDT key reused", e)
		}
		tokenNode.contract = ""
		if _, e := s.Bind(ctx, p.RequestKey, p.Signature); !errors.Is(e, chainsend.ErrInvalidRequest) {
			t.Fatal("signed after contract changed", e)
		}
		tokenNode.contract = p.TokenContract
	}
	if err != nil || prepared.TxId != req.Hash {
		t.Fatal(prepared, err)
	}
	if _, err = s.Bind(ctx, p.RequestKey, p.Signature); err != nil {
		t.Fatal(err)
	}
	evidence := src.proof
	node.head.Timestamp = p.ExpirationMs
	src.proof = chainassets.TRONEvidence{Genesis: evidence.Genesis, Transaction: json.RawMessage("{}"), Info: json.RawMessage("{}")}
	if row, err := s.Step(ctx, p.RequestKey); !errors.Is(err, chainsend.ErrTRONExpired) || row.State != chainsend.Signed || row.Attempts != 0 {
		t.Fatal("expired signed authorization sent", row, err)
	}
	src.proof = evidence
	// Even a valid txID is no proof for a different signer or asset amount.
	editBaseJSON(t, &src.proof.Transaction, func(m map[string]any) { m["signature"] = []any{} })
	if _, err = s.Step(ctx, p.RequestKey); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
		t.Fatal("wrong signature", err)
	}
	src.proof = evidence
	record := func(sender string, wrongAmount bool) error {
		if p.TokenContract != "" {
			atoms := p.TokenAtoms
			if wrongAmount {
				atoms = "999"
			}
			_, err := receipts.RecordUSDTPayout(ctx, p.TxId, sender, p.Recipient, p.TokenContract, atoms, p.Signature)
			return err
		}
		amount := p.Sun
		if wrongAmount {
			amount++
		}
		_, err := receipts.RecordNativePayout(ctx, p.TxId, sender, p.Recipient, amount, p.Signature)
		return err
	}
	if err = record(p.Recipient, false); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
		t.Fatal("wrong fee payer", err)
	}
	if err = record(p.Sender, true); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
		t.Fatal("wrong amount", err)
	}
	tokenNode.contract = "" // An original solidified authorization stays reconcilable.
	mustExec(t, db, "ALTER TABLE tron_payout ADD CONSTRAINT tron_test_pause CHECK (state<>'confirmed')")
	if _, err = s.Step(ctx, p.RequestKey); err == nil {
		t.Fatal("missing confirmation fault")
	}
	got, err := receipts.Find(ctx, chainassets.TRONNile, p.TxId, capture.Receipt.Event.Kind, capture.Receipt.Event.Instruction)
	if err != nil || got.Transaction != capture.Receipt.Transaction || got.Event != capture.Receipt.Event {
		t.Fatal("archived receipt mismatch", got, err)
	}
	mustExec(t, db, "ALTER TABLE tron_payout DROP CHECK tron_test_pause")
	proof.err = io.EOF
	row, err := open().Step(ctx, p.RequestKey)
	if err != nil || row.State != chainsend.TRONConfirmed || row.TxId != p.TxId || row.Attempts != 0 || row.TransferLogIndex != p.TransferLogIndex {
		t.Fatal(row, err)
	}
	if _, err = open().Step(ctx, p.RequestKey); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"tron_payout": 1, "chain_observed_transaction": 1, "chain_transfer_event": 1, "event_outbox": 0, "ledger_voucher": 0} {
		var count int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
			t.Fatal(table, count, err)
		}
	}
}

func TestTRONPayoutMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "nile_payout_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, v := range []uint{28, 29, 28, 29} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(t, db, "INSERT INTO tron_payout(request_key,network,sender,recipient,sun,ref_block_id,ref_height,timestamp_ms,expiration_ms,raw_hex,tx_id,state) VALUES ('intent','tron-nile','sender','recipient',1,REPEAT('1',64),1,1,2,'01',REPEAT('a',64),'prepared')")
	if err = m.Migrate(28); err == nil {
		t.Fatal("unprocessed payout discarded")
	}
	var key string
	if err = db.QueryRow("SELECT request_key FROM tron_payout").Scan(&key); err != nil || key != "intent" {
		t.Fatal(key, err)
	}
}

func TestTRONUSDTPayoutMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "nile_usdt_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(29); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/tron-payout-trx.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Intent model.TronPayout }
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	p := f.Intent
	_, err = db.Exec("INSERT INTO tron_payout (request_key, network, sender, recipient, sun, ref_block_id, ref_height, timestamp_ms, expiration_ms, raw_hex, tx_id, signature, state) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)", p.RequestKey, p.Network, p.Sender, p.Recipient, p.Sun, p.RefBlockId, p.RefHeight, p.TimestampMs, p.ExpirationMs, p.RawHex, p.TxId, p.Signature, chainsend.Signed)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []uint{30, 29, 30} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(err)
		}
	}
	_, source := tronReceiptFixture(t, "tron-payout-trx")
	conn := sqlx.NewSqlConnFromDB(db)
	receipts := chainreceipt.NewTRON(conn, transaction.New(db), source)
	node := &tronUSDTNode{tronSendNode: &tronSendNode{head: tronnative.Head{ID: p.RefBlockId, Height: p.RefHeight, Timestamp: p.TimestampMs}}, contract: "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf"}
	s, err := chainsend.NewTRON(conn, p.Sender, node, receipts)
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.Step(ctx, p.RequestKey)
	if err != nil || row.State != chainsend.TRONConfirmed || row.AssetId != "tron-nile:trx" || row.Signature != p.Signature || row.TxId != p.TxId || row.TransferLogIndex.Valid {
		t.Fatal("migrated TRX authorization", row, err)
	}
	token, err := s.PrepareUSDT(ctx, "retain-usdt", p.Recipient, node.contract, "1000", 3000000)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Migrate(29); err == nil {
		t.Fatal("downgrade discarded a token authorization")
	}
	got, err := s.Find(ctx, token.RequestKey)
	if err != nil || got.TxId != token.TxId || got.TokenContract != node.contract {
		t.Fatal("downgrade lost token history", got, err)
	}
}
