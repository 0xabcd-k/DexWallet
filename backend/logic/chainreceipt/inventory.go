package chainreceipt

import (
	"context"
	"fmt"
	"slices"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	model "github.com/0xabcd-k/DexWallet/backend/model/chainreceipt"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type ReceiveAddressImport struct {
	Network  string `json:"network"`
	Added    int    `json:"added"`
	Existing int    `json:"existing"`
}

// ImportSingleUse registers an operator's verified owners as one transaction.
// Normalize and acquire unique keys in the same order for overlapping imports.
func (a *Attribution) ImportSingleUse(ctx context.Context, tx Transactor, network string, owners []string) (ReceiveAddressImport, error) {
	if !a.enabled(network, "") || len(owners) == 0 {
		return ReceiveAddressImport{}, ErrInvalidRequest
	}
	ordered := make([]string, len(owners))
	for i, raw := range owners {
		owner, err := receiveOwner(network, raw)
		if err != nil {
			return ReceiveAddressImport{}, fmt.Errorf("address entry %d: %w", i+1, err)
		}
		ordered[i] = owner
	}
	slices.Sort(ordered)
	ordered = slices.Compact(ordered)

	result := ReceiveAddressImport{Network: network}
	err := tx.TransactCtx(ctx, func(ctx context.Context, session sqlx.Session) error {
		addresses := model.NewChainReceiveAddressModel(sqlx.NewSqlConnFromSession(session))
		for _, owner := range ordered {
			added, err := registerOwner(ctx, addresses, network, owner, SingleUseAddress)
			if err != nil {
				return err
			}
			if added {
				result.Added++
			} else {
				result.Existing++
			}
		}
		return nil
	})
	if err != nil {
		return ReceiveAddressImport{}, err
	}
	return result, nil
}

type ReceiveAddressInventory struct {
	Network   string `json:"network"`
	Total     int64  `json:"total"`
	Allocated int64  `json:"allocated"`
	Available int64  `json:"available"`
}

// Inventory counts dedicated addresses in a single snapshot. Any request,
// including one that expired, permanently removes its address from availability.
func (a *Attribution) Inventory(ctx context.Context, network string) (ReceiveAddressInventory, error) {
	if !slices.ContainsFunc(a.networks, func(n chainassets.Network) bool { return n.ID == network && n.Testnet }) {
		return ReceiveAddressInventory{}, ErrInvalidRequest
	}
	var counts struct {
		Total     int64 `db:"total"`
		Allocated int64 `db:"allocated"`
	}
	err := a.conn.QueryRowCtx(ctx, &counts, `SELECT COUNT(*) AS total,
		COALESCE(SUM(EXISTS(SELECT 1 FROM chain_receive_request r WHERE r.address_id=a.id)),0) AS allocated
		FROM chain_receive_address a WHERE a.network=? AND a.route=?`, network, SingleUseAddress)
	if err != nil {
		return ReceiveAddressInventory{}, err
	}
	return ReceiveAddressInventory{Network: network, Total: counts.Total, Allocated: counts.Allocated, Available: counts.Total - counts.Allocated}, nil
}
