// Package event appends durable business facts to the caller's SQL transaction.
package event

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type Kind string

const TransferCompleted Kind = "transfer.completed"
const TransferVersion int64 = 1

// Integer identities are internal; the MQ envelope carries the event's public idx.
type Transfer struct {
	SenderID    int64  `json:"senderId,string"`
	RecipientID int64  `json:"recipientId,string"`
	VoucherID   int64  `json:"voucherId,string"`
	Amount      string `json:"amount"`
	Unit        string `json:"unit"`
}

// Append never commits and never publishes. Duplicate sources are errors: callers
// must recover the original business order rather than commit unrelated writes.
func Append(ctx context.Context, session sqlx.Session, kind Kind, businessID, version int64, payload any) error {
	if err := transaction.RequireSession(ctx, session); err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("event payload: %w", err)
	}
	_, err = session.ExecCtx(ctx, "INSERT INTO event_outbox(kind,business_id,version,payload,last_error,retry_reason) VALUES(?,?,?,?,?,?)", string(kind), businessID, version, string(data), "", "")
	return err
}
