package chainreceipt

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

// The solidity receipt reports net_usage and TRX burned. It does not identify
// the delegator; the execution service checks the platform's stake before submit.
func VerifyNileDeposit(e chainassets.TRONEvidence, q chainassets.DepositPreview, signature string) (Receipt, int64, error) {
	t, err := q.NileDepositTransfer()
	if err != nil {
		return Receipt{}, 0, err
	}
	asset := chainassets.Asset{ID: chainassets.TRONNileTRX, Origin: chainassets.Native, Decimals: 6}
	r := TRONRequest{Network: chainassets.TRONNile, Hash: q.MessageSHA256, AssetID: asset.ID, Recipient: t.Recipient, Atoms: strconv.FormatInt(t.Sun, 10)}
	receipt, err := verifyTRON(r, asset, e)
	failed := errors.Is(err, ErrExecutionFailed)
	if err != nil && !failed {
		if errors.Is(err, ErrUnconfirmed) && tronEmptyObject(e.Transaction) && tronEmptyObject(e.Info) {
			return Receipt{}, 0, ErrTRONSolidAbsent
		}
		return Receipt{}, 0, err
	}
	var signed struct{ Signature []string }
	var info tronInfo
	if decodeErr := json.Unmarshal(e.Transaction, &signed); decodeErr != nil {
		return Receipt{}, 0, decodeErr
	}
	if len(signed.Signature) != 1 || !strings.EqualFold(signed.Signature[0], signature) {
		return Receipt{}, 0, bad("solid Nile transfer differs from signed deposit")
	}
	if decodeErr := json.Unmarshal(e.Info, &info); decodeErr != nil {
		return Receipt{}, 0, decodeErr
	}
	res := info.Receipt
	if failed {
		// verifyTRON has checked the original hash, its raw data, canonical block
		// membership and failure status. Retain costs for review without a credit.
		receipt.Transaction.Slot = int64(info.Height)
		receipt.Transaction.FeeAtoms = strconv.FormatInt(int64(info.Fee), 10)
		return receipt, int64(res.NetUsage), ErrExecutionFailed
	}
	if receipt.Event.SourceOwner != t.Sender || receipt.Transaction.FeePayer != t.Sender {
		return Receipt{}, 0, bad("Nile deposit sender differs from signed transfer")
	}
	if res.EnergyUsageTotal != 0 || res.EnergyPenaltyTotal != 0 || res.EnergyFee != 0 {
		return Receipt{}, 0, bad("unexpected Energy for native TRX")
	}
	return receipt, int64(res.NetUsage), nil
}
