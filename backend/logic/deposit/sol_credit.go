package deposit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/logic/account"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	accounts "github.com/0xabcd-k/DexWallet/backend/model/account"
	model "github.com/0xabcd-k/DexWallet/backend/model/deposit"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// The ledger source uses the numeric order ID. The public bill displays the
// same order idx as the signed memo. Historical deposit/post entries are separate.
const ActionCreditSol account.Action = "credit_sol"
const ActionCreditUSDC account.Action = "credit_usdc"
const ActionCreditTestUSDT account.Action = "credit_test_usdt"

var ErrSolNotFinalized = errors.New("successful finalized SOL deposit required")

// CreditSol uses only the persisted verifier result and the accepted price.
// No signer, RPC, current market price or beneficiary supplied by the caller.
func CreditSol(ctx context.Context, tx *transaction.Runner, ledger *account.Service, quoteIdx string) (*model.SolDepositOrder, error) {
	if !idxSyntax.MatchString(quoteIdx) {
		return nil, errors.New("invalid SOL quote idx")
	}
	var result *model.SolDepositOrder
	err := tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		conn := sqlx.NewSqlConnFromSession(session)
		rows := model.NewSolDepositOrderModel(conn)
		order, err := rows.LockByQuoteIdx(ctx, quoteIdx)
		if err != nil {
			return err
		}
		if order.ExecutionState != SolFinalized || order.SponsorPolicy != devnetSponsorPolicy {
			return ErrSolNotFinalized
		}
		if order.CreditVoucherId.Valid {
			result = order
			return nil
		}
		quote, err := model.NewDepositQuoteConfirmationModel(conn).FindOne(ctx, quoteIdx)
		if err != nil {
			return err
		}
		var q chainassets.DepositPreview
		if err = json.Unmarshal([]byte(quote.SnapshotJson), &q); err != nil {
			return err
		}
		if q.AssetID != order.AssetId || q.QuoteIdx != order.QuoteIdx || q.SolOrderIdx != order.OrderIdx || q.MessageSHA256 != order.MessageSha256 || q.SolMessage != order.MessageBase64 {
			return errors.New("finalized order differs from confirmed SOL quote")
		}
		net, err := q.FinalizedSolUSD(order.ActualLamportsFee, order.ActualFundingLamports, order.UserSignature)
		if err != nil {
			return err
		}
		// A business-39 receipt may already have been assigned by the general
		// collection listener. Leave that case for reconciliation.
		var assigned bool
		err = session.QueryRowCtx(ctx, &assigned, `SELECT EXISTS(SELECT 1 FROM chain_observed_transaction t
			JOIN chain_transfer_event e ON e.transaction_id=t.id JOIN chain_receive_request r ON r.matched_event_id=e.id
			WHERE t.network=? AND t.signature=? AND e.event_kind=?)`,
			chainassets.SolanaDevnet, order.TxSignature.String, chainreceipt.SolanaInstruction)
		if err != nil {
			return err
		}
		if assigned {
			return fmt.Errorf("SOL deposit %s: %w", order.OrderIdx, chainreceipt.ErrReceiveConflict)
		}
		action := ActionCreditSol
		switch {
		case q.AssetID == chainassets.SolanaDevnetUSDC:
			action = ActionCreditUSDC
		case strings.HasPrefix(q.AssetID, chainassets.SolanaDevnetTestUSDTPrefix):
			action = ActionCreditTestUSDT
		}
		creditUser := quote.UserId
		if q.Beneficiary != nil {
			creditUser = q.Beneficiary.UserID
		}
		voucher, err := ledger.Post(ctx, session, account.Posting{
			Source:  account.Source{Type: account.BusinessDeposit, ID: order.Id, Action: action},
			Entries: []account.Entry{{System: account.SystemUSDClearing, Side: account.SideDebit, Amount: net}, account.Credit(creditUser, accounts.KindUSD, net)},
		})
		if err != nil {
			return err
		}
		if err = rows.MarkCredited(ctx, order.Id, voucher, net); err != nil {
			return err
		}
		result, err = rows.FindOne(ctx, order.Id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
