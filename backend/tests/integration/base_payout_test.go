//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"strings"
	"sync"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type basePayoutNode struct {
	nonce  uint64
	submit func(*types.Transaction) error
}

func (n *basePayoutNode) PendingBaseNonce(context.Context, string) (uint64, error) {
	return n.nonce, nil
}
func (n *basePayoutNode) SubmitBaseETH(_ context.Context, tx *types.Transaction) error {
	return n.submit(tx)
}

type basePayoutSource struct {
	*baseReceiptSource
	err error
}

func (s *basePayoutSource) ReadBaseEvidence(ctx context.Context, network, hash string) (chainassets.BaseEvidence, error) {
	if s.err != nil {
		return chainassets.BaseEvidence{}, s.err
	}
	return s.baseReceiptSource.ReadBaseEvidence(ctx, network, hash)
}

func TestBasePayoutOriginalAuthorizationAndRestart(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "base_payout")
	conn := sqlx.NewSqlConnFromDB(db)
	req, fixture := baseReceiptFixture(t, "base-payout-eth")
	source := &basePayoutSource{baseReceiptSource: fixture}
	evidence := fixture.proof
	var tx types.Transaction
	if err := json.Unmarshal(evidence.Transaction, &tx); err != nil {
		t.Fatal(err)
	}
	signer := types.NewLondonSigner(big.NewInt(chainassets.BaseSepoliaChainID))
	from, err := types.Sender(signer, &tx)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	receipts := chainreceipt.NewBase(conn, transaction.New(db), source)
	node := &basePayoutNode{nonce: tx.Nonce()}
	open := func() *chainsend.Base {
		s, err := chainsend.NewBase(conn, from.Hex(), node, receipts)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	const key = "base-eth-payout-regression"
	prepare := func(key string, gas int64) error {
		_, err := s.Prepare(ctx, key, chainsend.BaseTransfer{Recipient: req.Recipient, WeiAmount: req.Atoms, GasLimit: gas, FeeCap: tx.GasFeeCap().String(), TipCap: tx.GasTipCap().String()})
		return err
	}
	gas := int64(tx.Gas())
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := prepare(key, gas); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if err := prepare("second-key", gas); !errors.Is(err, chainsend.ErrBaseNonceReserved) {
		t.Fatal("nonce reused", err)
	}
	if err := prepare(key, gas+1); !errors.Is(err, chainsend.ErrConflict) {
		t.Fatal("fee policy changed", err)
	}
	if _, err = s.Step(ctx, key); !errors.Is(err, chainsend.ErrInvalidSignature) {
		t.Fatal(err)
	}
	if _, err = s.Bind(ctx, key, "0x02deadbeef"); !errors.Is(err, chainsend.ErrInvalidSignature) {
		t.Fatal(err)
	}
	row, err := s.Bind(ctx, key, hexutil.Encode(wire))
	if err != nil || row.TxHash != req.Hash || row.SigningHash != signer.Hash(&tx).Hex() {
		t.Fatal(row, err)
	}
	calls := 0
	node.submit = func(sent *types.Transaction) error {
		calls++
		raw, err := sent.MarshalBinary()
		if err != nil || !bytes.Equal(raw, wire) {
			t.Fatalf("broadcast changed original bytes: %v", err)
		}
		r, err := s.Find(ctx, key)
		if err != nil || r.TxHash != req.Hash || r.Attempts != int64(calls) {
			t.Fatalf("missing durable authorization: %v", err)
		}
		if calls == 1 {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	absent := chainassets.BaseEvidence{Transaction: json.RawMessage("null"), Receipt: json.RawMessage("null")}
	source.err = io.EOF
	if _, err = s.Step(ctx, key); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	source.err = nil
	source.proof = chainassets.BaseEvidence{Transaction: evidence.Transaction, Receipt: absent.Receipt}
	if _, err = s.Step(ctx, key); !errors.Is(err, chainreceipt.ErrUnconfirmed) {
		t.Fatal(err)
	}
	source.proof = absent
	node.nonce++
	if _, err = s.Step(ctx, key); !errors.Is(err, chainsend.ErrBaseNonceChanged) {
		t.Fatal(err)
	}
	node.nonce--
	if calls != 0 {
		t.Fatal("ambiguous or consumed nonce replayed")
	}
	if _, err = s.Step(ctx, key); !errors.Is(err, chainsend.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	s = open()
	row, err = s.Step(ctx, key)
	if err != nil || row.State != chainsend.Submitted || calls != 2 {
		t.Fatal(row, err, calls)
	}
	// A node can drop an acknowledged transaction. Explicit absence at the
	// original pending nonce may replay exactly the same bytes.
	row, err = s.Step(ctx, key)
	if err != nil || row.TxHash != req.Hash || calls != 3 {
		t.Fatal(row, err, calls)
	}
	source.proof = evidence
	editBaseJSON(t, &source.proof.Transaction, func(v map[string]any) { v["maxFeePerGas"] = "0x1" })
	if _, err = s.Step(ctx, key); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
		t.Fatal(err)
	}
	source.proof = evidence
	editBaseJSON(t, &source.proof.Finalized, func(v map[string]any) { v["number"] = "0x1" })
	if _, err = s.Step(ctx, key); !errors.Is(err, chainreceipt.ErrUnconfirmed) {
		t.Fatal(err)
	}
	source.proof = evidence
	editBaseJSON(t, &source.proof.Receipt, func(v map[string]any) { v["status"] = "0x0" })
	if _, err = s.Step(ctx, key); !errors.Is(err, chainreceipt.ErrExecutionFailed) {
		t.Fatal(err)
	}
	source.proof = evidence
	mustExec(t, db, "ALTER TABLE base_payout ADD CONSTRAINT base_test_finish CHECK (state<>'confirmed')")
	if _, err = s.Step(ctx, key); err == nil {
		t.Fatal("missing local confirmation fault")
	}
	got, err := receipts.Find(ctx, chainassets.BaseSepolia, req.Hash, chainreceipt.EVMValue, 0)
	if err != nil || got.Event.Atoms != req.Atoms || got.Event.SourceOwner != strings.ToLower(from.Hex()) {
		t.Fatal(got, err)
	}
	// The public Sepolia fixture separates execution, L1 data and operator costs in wei.
	if got.Transaction.ExecutionFeeAtoms != "126330000000" || got.Transaction.L1FeeAtoms != "163" || got.Transaction.OperatorFeeAtoms != "0" || got.Transaction.FeeAtoms != "126330000163" {
		t.Fatal("unexpected finalized network fees", got.Transaction)
	}
	mustExec(t, db, "ALTER TABLE base_payout DROP CHECK base_test_finish")
	source.err = io.EOF
	row, err = open().Step(ctx, key)
	if err != nil || row.State != chainsend.BaseConfirmed || row.TxHash != req.Hash || row.Attempts != 3 {
		t.Fatal(row, err)
	}
	if _, err = open().Step(ctx, key); err != nil || calls != 3 {
		t.Fatal(err, calls)
	}
	for table, want := range map[string]int{"base_payout": 1, "chain_transfer_event": 1, "chain_observed_transaction": 1, "ledger_voucher": 0, "event_outbox": 0} {
		var n int
		if err = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != want {
			t.Fatal(table, n, err)
		}
	}
}

func TestBasePayoutMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "base_payout_migrate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, v := range []uint{25, 26, 25, 26} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(t, db, "INSERT INTO base_payout(request_key,chain_id,sender,recipient,wei_amount,nonce,gas_limit,fee_cap,tip_cap,signing_hash,state) VALUES ('native',84532,'sender','recipient','1','0',21000,'1','0',REPEAT('a',66),'prepared')")
	if err = m.Migrate(25); err == nil {
		t.Fatal("downgrade discarded a reserved nonce")
	}
	var key string
	if err = db.QueryRow("SELECT request_key FROM base_payout").Scan(&key); err != nil || key != "native" {
		t.Fatal(key, err)
	}
}
