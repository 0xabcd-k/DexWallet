//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func recordCollectionFixture(t *testing.T, db *sql.DB, fixture string) chainreceipt.Receipt {
	t.Helper()
	ctx, conn, runner := context.Background(), sqlx.NewSqlConnFromDB(db), transaction.New(db)
	var saved chainreceipt.Stored
	var err error
	switch {
	case strings.HasPrefix(fixture, "base-"):
		request, source := baseReceiptFixture(t, fixture)
		saved, err = chainreceipt.NewBase(conn, runner, source).Record(ctx, request)
	case strings.HasPrefix(fixture, "tron-"):
		request, source := tronReceiptFixture(t, fixture)
		saved, err = chainreceipt.NewTRON(conn, runner, source).Record(ctx, request)
	case strings.HasPrefix(fixture, "ton-"):
		request, source := tonReceiptFixture(t, fixture)
		saved, err = chainreceipt.NewTON(conn, runner, source).Record(ctx, request)
	default:
		request, source := receiptFixtureSource(t, fixture)
		saved, err = chainreceipt.New(conn, runner, source).Record(ctx, request)
	}
	if err != nil {
		t.Fatal(err)
	}
	return saved.Receipt
}

func collectionSetup(db *sql.DB, receipt chainreceipt.Receipt) (*chainreceipt.Attribution, chainreceipt.ReceiveTerms, chainreceipt.EventKey) {
	tx, ev := receipt.Transaction, receipt.Event
	n := chainassets.Network{ID: tx.Network, Testnet: true, Enabled: true, Assets: []chainassets.Asset{{ID: ev.AssetID}}}
	return chainreceipt.NewAttribution(sqlx.NewSqlConnFromDB(db), []chainassets.Network{n}),
		chainreceipt.ReceiveTerms{Key: "attempt-a", Purpose: chainreceipt.ReceiveDeposit, BeneficiaryID: 18, Payer: ev.SourceOwner, Recipient: ev.RecipientOwner, Network: tx.Network, AssetID: ev.AssetID, Atoms: ev.Atoms, ExpiresAt: time.Now().Add(time.Hour).Unix()},
		chainreceipt.EventKey{Network: tx.Network, Signature: tx.Signature, Kind: ev.Kind, Index: ev.Instruction}
}

func TestReceiveAttributionDedicated(t *testing.T) {
	for i, name := range []string{"sol", "usdc", "usdt-fixture", "base-eth", "base-internal-eth", "base-usdc", "tron-trx", "tron-usdt", "ton-collection-native", "ton-collection-jetton"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db, _ := userDatabase(t, fmt.Sprintf("receive_case_%d", i))
			receipt := recordCollectionFixture(t, db, name)
			service, terms, key := collectionSetup(db, receipt)
			if _, err := service.Match(ctx, key); !errors.Is(err, chainreceipt.ErrNoReceiveRequest) {
				t.Fatal(err)
			}
			if err := service.Register(ctx, terms.Network, terms.Recipient, chainreceipt.SingleUseAddress); err != nil {
				t.Fatal(err)
			}
			auto := terms
			auto.Recipient = ""
			if _, err := service.Open(ctx, auto); err != nil {
				t.Fatal(err)
			}
			again, err := service.Open(ctx, terms)
			if err != nil || again.ReceiveTerms != terms || again.Reference != "" || again.Event != nil {
				t.Fatal(again, err)
			}
			changed := terms
			changed.BeneficiaryID++
			if _, err = service.Open(ctx, changed); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
				t.Fatal(err)
			}
			// Replay a request issued just before the historical payment. Time and
			// terms can only be seeded directly in this isolated integration database.
			mustExec(t, db, "UPDATE chain_receive_request SET opens_at=1, expires_at=2")
			if _, err = service.Match(ctx, key); !errors.Is(err, chainreceipt.ErrReceiveWindow) {
				t.Fatal(err)
			}
			mustExec(t, db, "UPDATE chain_receive_request SET expires_at=4102444800")
			restored := chainreceipt.NewAttribution(sqlx.NewSqlConnFromDB(db), nil)
			for j := 0; j < 2; j++ {
				got, e := restored.Match(ctx, key)
				if e != nil || got.Key != terms.Key || got.Event == nil || *got.Event != key || got.BeneficiaryID != terms.BeneficiaryID {
					t.Fatal(got, e)
				}
			}
			changed.Key = "different-order"
			changed.Purpose = chainreceipt.ReceiveMerchantPayment
			if _, err = service.Open(ctx, changed); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
				t.Fatal(err)
			}
			for _, table := range []string{"user_account", "ledger_voucher", "event_outbox"} {
				var count int
				if e := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); e != nil || count != 0 {
					t.Fatal(table, count, e)
				}
			}
		})
	}
}

func TestReceiveAttributionSharedMemo(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "receive_shared")
	for _, name := range []string{"ton-collection-native", "ton-collection-jetton"} {
		receipt := recordCollectionFixture(t, db, name)
		service, terms, key := collectionSetup(db, receipt)
		raw, err := os.ReadFile("../../logic/chainreceipt/testdata/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var f struct{ Collection chainreceipt.ReceiveRequest }
		if err = json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		if err = service.Register(ctx, terms.Network, terms.Recipient, chainreceipt.TONMemo); err != nil {
			t.Fatal(err)
		}
		if err = service.Register(ctx, terms.Network, terms.Recipient, chainreceipt.SingleUseAddress); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
			t.Fatal(err)
		}
		terms.Key = "other-" + name
		other, err := service.Open(ctx, terms)
		if err != nil {
			t.Fatal(err)
		}
		terms.Key = name
		terms.Purpose = chainreceipt.ReceiveMerchantPayment
		terms.BeneficiaryID++
		issued, err := service.Open(ctx, terms)
		if err != nil || len(issued.Reference) != 35 || issued.Reference == other.Reference {
			t.Fatal(issued, err)
		}
		if _, err = service.Match(ctx, key); !errors.Is(err, chainreceipt.ErrNoReceiveRequest) {
			t.Fatal("unissued memo", err)
		}
		const paidAt = 1795257327
		mustExec(t, db, "UPDATE chain_receive_request SET payment_reference=?,opens_at=?,expires_at=? WHERE request_key=?", f.Collection.Reference, paidAt+1, paidAt+10, name)
		if _, err = service.Match(ctx, key); !errors.Is(err, chainreceipt.ErrReceiveWindow) {
			t.Fatal("prior payment", err)
		}
		mustExec(t, db, "UPDATE chain_receive_request SET opens_at=?,expires_at=? WHERE request_key=?", paidAt-1, paidAt, name)
		if _, err = service.Match(ctx, key); !errors.Is(err, chainreceipt.ErrReceiveWindow) {
			t.Fatal("exclusive expiry", err)
		}
		mustExec(t, db, "UPDATE chain_receive_request SET opens_at=?,expires_at=? WHERE request_key=?", paidAt, paidAt+1, name)
		for _, c := range []struct{ column, wrong, right string }{
			{"amount_atoms", "1", terms.Atoms}, {"asset_id", "another-asset", terms.AssetID}, {"payer_owner", terms.Recipient, terms.Payer},
		} {
			mustExec(t, db, "UPDATE chain_receive_request SET "+c.column+"=? WHERE request_key=?", c.wrong, name)
			if _, err = service.Match(ctx, key); !errors.Is(err, chainreceipt.ErrReceiveMismatch) {
				t.Fatal(c.column, err)
			}
			mustExec(t, db, "UPDATE chain_receive_request SET "+c.column+"=? WHERE request_key=?", c.right, name)
		}
		got, err := service.Match(ctx, key)
		if err != nil || got.Key != terms.Key || got.BeneficiaryID != terms.BeneficiaryID || got.Event == nil || *got.Event != key {
			t.Fatal(got, err)
		}
		pending, err := service.Find(ctx, other.Key)
		if err != nil || pending.Event != nil {
			t.Fatal("competing request claimed event", pending, err)
		}
	}
}

func TestReceiveAttributionConcurrent(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "receive_concurrent")
	conn := sqlx.NewSqlConnFromDB(db)
	req, src := receiptFixtureSource(t, "sol")
	editReceipt(t, src, func(m map[string]any) {
		msg := m["transaction"].(map[string]any)["message"].(map[string]any)
		ix := msg["instructions"].([]any)
		msg["instructions"] = append(ix, ix[0])
	})
	observations := chainreceipt.New(conn, transaction.New(db), src)
	saved, err := observations.Record(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	s, terms, e0 := collectionSetup(db, saved.Receipt)
	req.Instruction = 1
	if _, err = observations.Record(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err = s.Register(ctx, terms.Network, terms.Recipient, chainreceipt.SingleUseAddress); err != nil {
		t.Fatal(err)
	}
	expired := terms
	expired.ExpiresAt = 1
	if _, err = s.Open(ctx, expired); !errors.Is(err, chainreceipt.ErrReceiveWindow) {
		t.Fatal(err)
	}
	disabled := chainreceipt.NewAttribution(conn, nil)
	if _, err = disabled.Open(ctx, terms); !errors.Is(err, chainreceipt.ErrInvalidRequest) {
		t.Fatal(err)
	}
	const attempts = 12
	keys := make(chan string, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := terms
			r.Key = fmt.Sprintf("attempt-%d", i)
			got, e := s.Open(ctx, r)
			if e == nil {
				keys <- got.Key
			} else if !errors.Is(e, chainreceipt.ErrReceiveConflict) {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	close(keys)
	if len(keys) != 1 {
		t.Fatalf("issued %d requests for one address", len(keys))
	}
	terms.Key = <-keys
	mustExec(t, db, "UPDATE chain_receive_request SET opens_at=1,expires_at=4102444800")
	e1 := e0
	e1.Index = 1
	type result struct {
		key chainreceipt.EventKey
		err error
	}
	results := make(chan result, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := e0
			if i%2 == 1 {
				key = e1
			}
			_, err := s.Match(ctx, key)
			results <- result{key, err}
		}(i)
	}
	wg.Wait()
	close(results)
	final, err := disabled.Find(ctx, terms.Key)
	if err != nil || final.Event == nil {
		t.Fatal(final, err)
	}
	for v := range results {
		if v.key == *final.Event && v.err != nil || v.key != *final.Event && !errors.Is(v.err, chainreceipt.ErrReceiveConflict) {
			t.Fatal(v)
		}
	}
}

func TestReceiveAttributionPlatformSourceAndMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "receive_migration")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Migrate(31); err != nil {
		t.Fatal(err)
	}
	receipt := recordCollectionFixture(t, db, "sol")
	for _, v := range []uint{32, 31, 32} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(v, err)
		}
	}
	s, terms, key := collectionSetup(db, receipt)
	if err = s.Register(ctx, terms.Network, terms.Recipient, chainreceipt.SingleUseAddress); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Open(ctx, terms); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, "UPDATE chain_receive_request SET opens_at=1,expires_at=4102444800")
	if err = s.Register(ctx, terms.Network, terms.Payer, chainreceipt.SingleUseAddress); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Match(ctx, key); !errors.Is(err, chainreceipt.ErrReceiveMismatch) {
		t.Fatal(err)
	}
	terms.Key = "platform-funded"
	if _, err = s.Open(ctx, terms); !errors.Is(err, chainreceipt.ErrInvalidRequest) {
		t.Fatal(err)
	}
	if err = m.Migrate(31); err == nil || !strings.Contains(err.Error(), "receive_rollback_guard") {
		t.Fatal("rollback removed issued address", err)
	}
	found, err := s.Find(ctx, "attempt-a")
	if err != nil || found.Event != nil {
		t.Fatal(found, err)
	}
}
