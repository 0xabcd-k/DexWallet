//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/0xabcd-k/DexWallet/backend/internal/migration"
	"github.com/0xabcd-k/DexWallet/backend/internal/solanaspl"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type splPayoutNode struct {
	payoutRPC
	catalog []chainassets.Network
}

func (n *splPayoutNode) Catalog() []chainassets.Network { return n.catalog }
func (n *splPayoutNode) CheckSolanaTokenAccounts(context.Context, solanaspl.Transfer) error {
	return nil
}

func TestSolanaTokenPayoutRecovery(t *testing.T) {
	ctx := context.Background()
	db, _ := userDatabase(t, "spl_recovery")
	conn := sqlx.NewSqlConnFromDB(db)
	for _, name := range []string{"usdc-existing", "usdc-new", "usdt-fixture-existing", "usdt-fixture-new"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../../logic/chainreceipt/testdata/spl-payout-" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var capture payoutCapture
			if err = json.Unmarshal(raw, &capture); err != nil {
				t.Fatal(err)
			}
			intent := capture.Intent
			_, source := receiptFixtureSource(t, "sol")
			receipts := chainreceipt.New(conn, transaction.New(db), source)
			source.proof = capture.Evidence
			node := &splPayoutNode{catalog: source.Catalog(), payoutRPC: payoutRPC{block: chainassets.SolanaBlock{Blockhash: intent.Blockhash, LastValidBlockHeight: intent.LastValidHeight}, height: intent.LastValidHeight + 1, submit: func([]byte) error { t.Fatal("broadcast of finalized or expired payout"); return nil }}}
			prepare := func() *chainsend.Solana {
				s, err := chainsend.NewSolana(conn, intent.Sender, node, receipts)
				if err != nil {
					t.Fatal(err)
				}
				return s
			}
			s := prepare()
			create := intent.CreateAta == 1
			row, err := s.PrepareToken(ctx, intent.RequestKey, intent.Recipient, intent.AssetId, intent.TokenAtoms, create)
			if err != nil || row.MessageHash != intent.MessageHash {
				t.Fatal(row, err)
			}
			if _, err = s.PrepareToken(ctx, intent.RequestKey, intent.Recipient, intent.AssetId, intent.TokenAtoms, !create); !errors.Is(err, chainsend.ErrConflict) {
				t.Fatal("changed account creation authority", err)
			}
			if _, err = s.Prepare(ctx, intent.RequestKey, intent.Recipient, "1000"); !errors.Is(err, chainsend.ErrConflict) {
				t.Fatal("token/native confusion", err)
			}
			if _, err = s.PrepareToken(ctx, intent.RequestKey, intent.Recipient, "solana-mainnet:usdc", intent.TokenAtoms, create); err == nil {
				t.Fatal("mainnet asset accepted")
			}
			if _, err = s.Bind(ctx, intent.RequestKey, intent.Signature); err != nil {
				t.Fatal(err)
			}
			// No test can re-broadcast these historical authorizations. A mismatched proof must fail before recording a receipt.
			edits := []func(map[string]any){
				func(tx map[string]any) {
					balances := tx["meta"].(map[string]any)["postBalances"].([]any)
					balances[0] = 0
				},
				func(tx map[string]any) {
					balances := tx["meta"].(map[string]any)["postTokenBalances"].([]any)
					balances[0].(map[string]any)["uiTokenAmount"].(map[string]any)["amount"] = "0"
				},
				func(tx map[string]any) {
					msg := tx["transaction"].(map[string]any)["message"].(map[string]any)
					ix := msg["instructions"].([]any)
					msg["instructions"] = append(ix, ix[len(ix)-1])
				},
			}
			if create {
				edits = append(edits, func(tx map[string]any) {
					ix := tx["transaction"].(map[string]any)["message"].(map[string]any)["instructions"].([]any)[0].(map[string]any)
					ix["parsed"].(map[string]any)["info"].(map[string]any)["wallet"] = intent.Sender
				})
			}
			for i, edit := range edits {
				source.proof = capture.Evidence
				editReceipt(t, source, edit)
				if _, err = s.Step(ctx, intent.RequestKey); !errors.Is(err, chainreceipt.ErrInvalidEvidence) {
					t.Fatalf("proof %d: %v", i, err)
				}
			}
			source.proof = chainassets.SolanaEvidence{Transaction: json.RawMessage("null"), Status: json.RawMessage(`{"context":{"slot":10},"value":[null]}`)}
			if _, err = s.Step(ctx, intent.RequestKey); !errors.Is(err, chainsend.ErrSolanaExpired) {
				t.Fatal(err)
			}
			// Block only the local final marker after receipt commit, then turn off the RPC.
			source.proof = capture.Evidence
			mustExec(t, db, "ALTER TABLE solana_payout ADD CONSTRAINT spl_test_finish CHECK (state <> 'confirmed' OR request_key <> '"+intent.RequestKey+"')")
			if _, err = s.Step(ctx, intent.RequestKey); err == nil {
				t.Fatal("confirmation write fault hidden")
			}
			got, err := receipts.Find(ctx, chainassets.SolanaDevnet, intent.Signature, int(intent.CreateAta))
			if err != nil || got.Event != capture.Receipt.Event {
				t.Fatal(got, err)
			}
			mustExec(t, db, "ALTER TABLE solana_payout DROP CHECK spl_test_finish")
			source.proof = chainassets.SolanaEvidence{}
			row, err = prepare().Step(ctx, intent.RequestKey)
			if err != nil || row.State != chainsend.SolanaConfirmed || row.AtaFundingLamports != intent.AtaFundingLamports || row.Attempts != 0 {
				t.Fatal(row, err)
			}
		})
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM chain_transfer_event").Scan(&count); err != nil || count != 4 {
		t.Fatal(count, err)
	}
}

func TestSolanaPayoutTokenMigration(t *testing.T) {
	ctx := context.Background()
	name, dsn, db, err := testRun.NewDatabase(ctx, "spl_migrate")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := migration.Open(dsn, name, "../../manifest/sql/diff/business", "business")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, v := range []uint{23, 24, 23} {
		if err = m.Migrate(v); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(t, db, "INSERT INTO solana_payout(request_key,sender,recipient,lamports,blockhash,last_valid_height,message_hash,state) VALUES('test','sender','recipient','1','hash',1,REPEAT('a',64),'prepared')")
	for _, v := range []uint{24, 23, 24} {
		if err = m.Migrate(v); err != nil {
			t.Fatal("native history round trip", err)
		}
	}
	var asset string
	if err = db.QueryRow("SELECT asset_id FROM solana_payout WHERE request_key='test'").Scan(&asset); err != nil || asset != "solana-devnet:sol" {
		t.Fatal(asset, err)
	}
	mustExec(t, db, "INSERT INTO solana_payout(request_key,sender,recipient,lamports,asset_id,mint,token_atoms,blockhash,last_valid_height,message_hash,state) VALUES('token','sender','recipient','0','solana-devnet:usdc','mint','1000','hash',1,REPEAT('b',64),'prepared')")
	if err = m.Migrate(23); err == nil {
		t.Fatal("downgrade erased token history")
	}
	var retained string
	if err = db.QueryRow("SELECT token_atoms FROM solana_payout WHERE request_key='token'").Scan(&retained); err != nil || retained != "1000" {
		t.Fatal(retained, err)
	}

	var key string
	if err = db.QueryRow("SELECT request_key FROM solana_payout WHERE request_key='test'").Scan(&key); err != nil || key != "test" {
		t.Fatal(key, err)
	}
}
