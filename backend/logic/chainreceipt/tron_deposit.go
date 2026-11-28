package chainreceipt

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

type NileDepositUsage struct {
	Bandwidth, Energy int64
	Covered           bool
}

// The solidity receipt reports net_usage and TRX burned. It does not identify
// the delegator; the execution service checks the platform's stake before submit.
func VerifyNileDeposit(e chainassets.TRONEvidence, q chainassets.DepositPreview, signature string) (Receipt, NileDepositUsage, error) {
	t, err := q.NileDepositTransfer()
	if err != nil {
		return Receipt{}, NileDepositUsage{}, err
	}
	asset := chainassets.Asset{ID: chainassets.TRONNileTRX, Origin: chainassets.Native, Decimals: 6}
	r := TRONRequest{Network: chainassets.TRONNile, Hash: q.MessageSHA256, AssetID: asset.ID, Recipient: t.Recipient, Atoms: strconv.FormatInt(t.Sun, 10)}
	if t.TokenContract != "" {
		asset = chainassets.NileUSDT(t.TokenContract)
		r.AssetID, r.Atoms = asset.ID, t.TokenAtoms
	}
	receipt, err := verifyTRON(r, asset, e)
	failed := errors.Is(err, ErrExecutionFailed)
	if err != nil && !failed {
		if errors.Is(err, ErrUnconfirmed) && tronEmptyObject(e.Transaction) && tronEmptyObject(e.Info) {
			return Receipt{}, NileDepositUsage{}, ErrTRONSolidAbsent
		}
		return Receipt{}, NileDepositUsage{}, err
	}
	var signed struct{ Signature []string }
	var info tronInfo
	if decodeErr := json.Unmarshal(e.Transaction, &signed); decodeErr != nil {
		return Receipt{}, NileDepositUsage{}, decodeErr
	}
	if len(signed.Signature) != 1 || !strings.EqualFold(signed.Signature[0], signature) {
		return Receipt{}, NileDepositUsage{}, bad("solid Nile transfer differs from signed deposit")
	}
	if decodeErr := json.Unmarshal(e.Info, &info); decodeErr != nil {
		return Receipt{}, NileDepositUsage{}, decodeErr
	}
	res := info.Receipt
	usage := NileDepositUsage{Bandwidth: int64(res.NetUsage), Energy: int64(res.EnergyUsageTotal), Covered: res.EnergyUsage == res.EnergyUsageTotal && res.OriginEnergyUsage == 0 && res.EnergyFee == 0 && res.NetFee == 0}
	if failed {
		// verifyTRON has checked the original hash, its raw data, canonical block
		// membership and failure status. Retain costs for review without a credit.
		receipt.Transaction.Slot = int64(info.Height)
		receipt.Transaction.FeeAtoms = strconv.FormatInt(int64(info.Fee), 10)
		return receipt, usage, ErrExecutionFailed
	}
	if receipt.Event.SourceOwner != t.Sender || receipt.Transaction.FeePayer != t.Sender {
		return Receipt{}, NileDepositUsage{}, bad("Nile deposit sender differs from signed transfer")
	}
	return receipt, usage, nil
}
