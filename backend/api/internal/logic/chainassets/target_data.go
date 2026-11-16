package chainassets

import (
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	domain "github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

func targetData(t domain.Target) types.ChainTargetData {
	return types.ChainTargetData{Network: t.Network, AssetId: t.AssetID, Address: t.Address, ReceiveCode: t.ReceiveCode}
}
