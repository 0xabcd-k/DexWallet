package chainsend

import (
	"context"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// Reserve serializes competing keys/nonces. Expired new proposals cannot reserve
// a nonce; an already persisted proposal can still be read after its expiry.
func Reserve(ctx context.Context, conn sqlx.SqlConn, r *ChainSendIntent) error {
	_, err := conn.ExecCtx(ctx, `INSERT INTO chain_send_intent(network,request_key,sender,recipient,asset_id,amount_atoms,public_key,subwallet_id,seqno,valid_until,payload_hash,state,last_error)
	 SELECT ?,?,?,?,?,?,?,?,?,?,?,?,'' FROM DUAL WHERE ? > UNIX_TIMESTAMP()
	 ON DUPLICATE KEY UPDATE id=id`, r.Network, r.RequestKey, r.Sender, r.Recipient, r.AssetId, r.AmountAtoms, r.PublicKey, r.SubwalletId, r.Seqno, r.ValidUntil, r.PayloadHash, r.State, r.ValidUntil)
	return err
}

func LockKey(ctx context.Context, conn sqlx.SqlConn, network, key string) (*ChainSendIntent, error) {
	var r ChainSendIntent
	err := conn.QueryRowCtx(ctx, &r, "SELECT "+chainSendIntentRows+" FROM chain_send_intent WHERE network=? AND request_key=? FOR UPDATE", network, key)
	return &r, err
}

func LockID(ctx context.Context, conn sqlx.SqlConn, id int64) (*ChainSendIntent, error) {
	var r ChainSendIntent
	err := conn.QueryRowCtx(ctx, &r, "SELECT "+chainSendIntentRows+" FROM chain_send_intent WHERE id=? FOR UPDATE", id)
	return &r, err
}

func Now(ctx context.Context, conn sqlx.SqlConn) (int64, error) {
	var now int64
	err := conn.QueryRowCtx(ctx, &now, "SELECT UNIX_TIMESTAMP()")
	return now, err
}

// UpdateExecution never changes the reserved payment facts. Caller holds its row lock.
func UpdateExecution(ctx context.Context, conn sqlx.SqlConn, r *ChainSendIntent) error {
	_, err := conn.ExecCtx(ctx, `UPDATE chain_send_intent SET signed_boc=?,message_hash=?,body_hash=?,state=?,attempts=?,last_error=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?`, r.SignedBoc, r.MessageHash, r.BodyHash, r.State, r.Attempts, r.LastError, r.Id)
	return err
}
