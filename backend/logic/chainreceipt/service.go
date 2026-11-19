package chainreceipt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type Transactor interface {
	TransactCtx(context.Context, func(context.Context, sqlx.Session) error) error
}
type Service struct {
	verifier *Verifier
	conn     sqlx.SqlConn
	tx       Transactor
}

func New(conn sqlx.SqlConn, tx Transactor, source Source) *Service {
	return &Service{NewVerifier(source), conn, tx}
}

type Stored struct {
	Receipt Receipt `json:"receipt"`
	Reused  bool    `json:"reused"`
}

// Record does not accept a client-created Receipt. Evidence is read/verified
// before opening the SQL transaction. No ledger, order or outbox write occurs.
func (s *Service) Record(ctx context.Context, r Request) (Stored, error) {
	receipt, err := s.verifier.Verify(ctx, r)
	if err != nil {
		return Stored{}, err
	}
	return s.record(ctx, receipt)
}

// Only verified facts from the chain-specific verifiers enter this shared store.
func (s *Service) record(ctx context.Context, receipt Receipt) (Stored, error) {
	evidence := receipt.evidence
	if base, ok := evidence.(chainassets.BaseEvidence); ok {
		// Trace is immutable supplementary evidence; existing receipt evidence stays untouched.
		base.CallTrace = nil
		evidence = base
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return Stored{}, err
	}
	var result Stored
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		conn := sqlx.NewSqlConnFromSession(session)
		t := receipt.Transaction
		row, err := model.LockTransaction(ctx, conn, &model.ChainObservedTransaction{Network: t.Network, Signature: t.Signature, Slot: t.Slot, FeePayer: t.FeePayer, FeeAtoms: t.FeeAtoms, BlockHash: t.BlockHash, ExecutionFeeAtoms: t.ExecutionFeeAtoms, L1FeeAtoms: t.L1FeeAtoms, OperatorFeeAtoms: t.OperatorFeeAtoms, Fingerprint: t.Fingerprint, EvidenceJson: string(raw)})
		if err != nil {
			return err
		}
		if transactionFacts(row) != t {
			return ErrConflict
		}
		if receipt.traceFingerprint != "" {
			if row.CallTraceFingerprint == "" {
				if err := model.SaveCallTrace(ctx, conn, row.Id, receipt.traceFingerprint, string(receipt.callTrace)); err != nil {
					return err
				}
			} else if row.CallTraceFingerprint != receipt.traceFingerprint {
				return ErrConflict
			}
		}
		events := model.NewChainTransferEventModel(conn)
		previous, err := events.FindOneByTransactionIdEventKindInstructionIndex(ctx, row.Id, receipt.Event.Kind, int64(receipt.Event.Instruction))
		if err == nil {
			if eventFacts(previous) != receipt.Event {
				return ErrConflict
			}
			result = Stored{Receipt: receipt, Reused: true}
			return nil
		}
		if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		e := receipt.Event
		_, err = events.Insert(ctx, &model.ChainTransferEvent{TransactionId: row.Id, EventKind: e.Kind, InstructionIndex: int64(e.Instruction), AssetId: e.AssetID, ProgramId: e.ProgramID, Mint: e.Mint, Decimals: int64(e.Decimals), SourceAccount: e.SourceAccount, SourceOwner: e.SourceOwner, RecipientAccount: e.RecipientAccount, RecipientOwner: e.RecipientOwner, AmountAtoms: e.Atoms})
		if err == nil {
			result = Stored{Receipt: receipt}
		}
		return err
	})
	if err == nil {
		return result, nil
	}
	var failure *transaction.Failure
	if errors.As(err, &failure) && failure.Unknown() {
		found, lookupErr := s.findEvent(ctx, receipt.Transaction.Network, receipt.Transaction.Signature, receipt.Event.Kind, receipt.Event.Instruction)
		if lookupErr == nil && found.Transaction == receipt.Transaction && found.Event == receipt.Event && found.traceFingerprint == receipt.traceFingerprint {
			return Stored{Receipt: found, Reused: true}, nil
		}
		if lookupErr == nil {
			lookupErr = ErrConflict
		}
		return Stored{}, fmt.Errorf("%w: %w", ErrOutcomeUnknown, errors.Join(err, lookupErr))
	}
	return Stored{}, err
}
func (s *Service) Find(ctx context.Context, network, signature string, instruction int) (Receipt, error) {
	return s.findEvent(ctx, network, signature, SolanaInstruction, instruction)
}
func (s *Service) findEvent(ctx context.Context, network, signature, kind string, instruction int) (Receipt, error) {
	row, err := model.NewChainObservedTransactionModel(s.conn).FindOneByNetworkSignature(ctx, network, signature)
	if err != nil {
		return Receipt{}, err
	}
	event, err := model.NewChainTransferEventModel(s.conn).FindOneByTransactionIdEventKindInstructionIndex(ctx, row.Id, kind, int64(instruction))
	if err != nil {
		return Receipt{}, err
	}
	found := Receipt{Transaction: transactionFacts(row), Event: eventFacts(event)}
	if kind == EVMCall {
		found.traceFingerprint = row.CallTraceFingerprint
	}
	return found, nil
}
func transactionFacts(t *model.ChainObservedTransaction) Transaction {
	return Transaction{Network: t.Network, Signature: t.Signature, Slot: t.Slot, FeePayer: t.FeePayer, FeeAtoms: t.FeeAtoms, BlockHash: t.BlockHash, ExecutionFeeAtoms: t.ExecutionFeeAtoms, L1FeeAtoms: t.L1FeeAtoms, OperatorFeeAtoms: t.OperatorFeeAtoms, Fingerprint: t.Fingerprint}
}
func eventFacts(e *model.ChainTransferEvent) Event {
	return Event{Kind: e.EventKind, Instruction: int(e.InstructionIndex), AssetID: e.AssetId, ProgramID: e.ProgramId, Mint: e.Mint, Decimals: int(e.Decimals), SourceAccount: e.SourceAccount, SourceOwner: e.SourceOwner, RecipientAccount: e.RecipientAccount, RecipientOwner: e.RecipientOwner, Atoms: e.AmountAtoms}
}
