//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

func TestReceiveAllocationConcurrent(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "receive_allocation")
	receipt := recordCollectionFixture(t, db, "base-eth")
	s, terms, _ := collectionSetup(db, receipt)
	terms.Recipient = ""
	register := func(id int) string {
		t.Helper()
		addr := fmt.Sprintf("0x%040x", id)
		if err := s.Register(ctx, terms.Network, addr, chainreceipt.SingleUseAddress); err != nil {
			t.Fatal(err)
		}
		return addr
	}
	type result struct {
		key    string
		issued chainreceipt.ReceiveRequest
		err    error
	}
	openTogether := func(keys []string) chan result {
		start := make(chan struct{})
		out := make(chan result, len(keys))
		for _, key := range keys {
			r := terms
			r.Key = key
			go func() {
				<-start
				issued, err := s.Open(ctx, r)
				out <- result{r.Key, issued, err}
			}()
		}
		close(start)
		return out
	}
	const attempts = 16
	keys := make([]string, attempts)
	for i := range keys {
		keys[i] = terms.Key
	}
	first := register(1)
	replays := openTogether(keys)
	for range keys {
		r := <-replays
		if r.err != nil || r.issued.Recipient != first || r.issued.Reference != "" || r.issued.Route != chainreceipt.SingleUseAddress {
			t.Fatalf("replay returned %+v", r)
		}
	}
	changed := terms
	changed.Atoms = "999"
	if _, err := s.Open(ctx, changed); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
		t.Fatal("changed amount", err)
	}
	disabled := chainreceipt.NewAttribution(sqlx.NewSqlConnFromDB(db), nil)
	if replay, err := disabled.Open(ctx, terms); err != nil || replay.Recipient != first {
		t.Fatal("restart after network disabled", replay, err)
	}
	mustExec(t, db, "UPDATE chain_receive_request SET opens_at=1,expires_at=2 WHERE request_key=?", terms.Key)

	const available = 8
	pool := make(map[string]bool)
	for i := 0; i < available; i++ {
		pool[register(i+2)] = true
	}
	terms.Purpose = chainreceipt.ReceiveMerchantPayment
	for i := range keys {
		keys[i] = fmt.Sprintf("order-%d", i)
	}
	orders := openTogether(keys)
	var waiting []string
	for range keys {
		r := <-orders
		switch {
		case errors.Is(r.err, chainreceipt.ErrReceiveAddressExhausted):
			waiting = append(waiting, r.key)
		case r.err != nil || !pool[r.issued.Recipient] || r.issued.Key != r.key:
			t.Errorf("unexpected allocation: %+v", r)
		default:
			delete(pool, r.issued.Recipient)
		}
	}
	if len(pool) != 0 || len(waiting) != attempts-available {
		t.Fatalf("unused addresses %v, waiting requests %v", pool, waiting)
	}
	next := register(99)
	terms.Key = waiting[0]
	got, err := s.Open(ctx, terms)
	if err != nil || got.Recipient != next {
		t.Fatal("retry after refill", got, err)
	}
}

func TestReceiveAllocationEligibility(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "receive_alloc_eligible")
	receipt := recordCollectionFixture(t, db, "ton-collection-native")
	s, terms, _ := collectionSetup(db, receipt)
	if err := s.Register(ctx, terms.Network, terms.Recipient, chainreceipt.TONMemo); err != nil {
		t.Fatal(err)
	}
	shared, err := s.Open(ctx, terms)
	if err != nil || shared.Reference == "" {
		t.Fatal(shared, err)
	}
	terms.Recipient = ""
	if _, err = s.Open(ctx, terms); !errors.Is(err, chainreceipt.ErrReceiveConflict) {
		t.Fatal("dedicated request cannot replay a shared destination", err)
	}
	terms.Key = "dedicated"
	baseReceipt := recordCollectionFixture(t, db, "base-eth")
	base, other, _ := collectionSetup(db, baseReceipt)
	if err = base.Register(ctx, chainassets.BaseSepolia, other.Recipient, chainreceipt.SingleUseAddress); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Open(ctx, terms); !errors.Is(err, chainreceipt.ErrReceiveAddressExhausted) {
		t.Fatal("only shared or other-network addresses in stock", err)
	}
	newOwner := fmt.Sprintf("0:%064x", 18)
	if err = s.Register(ctx, terms.Network, newOwner, chainreceipt.SingleUseAddress); err != nil {
		t.Fatal(err)
	}
	bad := terms
	bad.Payer = shared.Recipient
	if _, err = s.Open(ctx, bad); !errors.Is(err, chainreceipt.ErrInvalidRequest) {
		t.Fatal("platform payer", err)
	}
	bad = terms
	bad.ExpiresAt = time.Now().Add(-time.Second).Unix()
	if _, err = s.Open(ctx, bad); !errors.Is(err, chainreceipt.ErrReceiveWindow) {
		t.Fatal("expired request", err)
	}
	mustExec(t, db, "ALTER TABLE chain_receive_request ADD CONSTRAINT ck_test_issue CHECK (request_key <> 'dedicated')")
	if issued, err := s.Open(ctx, terms); err == nil || issued.Recipient != "" {
		t.Fatal("failed insert returned an address", issued, err)
	}
	mustExec(t, db, "ALTER TABLE chain_receive_request DROP CHECK ck_test_issue")
	issued, err := s.Open(ctx, terms)
	if err != nil || issued.Recipient != newOwner || issued.Reference != "" {
		t.Fatal("retry after failed issuance", issued, err)
	}
}
