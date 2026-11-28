package baseeth

import (
	"bytes"
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Independent historical vector: MetaMask 13.53 issued this permission and
// the deployed manager redeemed it on Base Sepolia before this deposit route existed.
func TestGrantMatchesMetaMaskRedemption(t *testing.T) {
	var f struct {
		Evidence struct {
			Transaction struct{ Input hexutil.Bytes }
			Receipt     struct {
				Logs []struct {
					Data   hexutil.Bytes
					Topics []common.Hash
				}
			}
		}
	}
	raw, err := os.ReadFile("../../logic/chainreceipt/testdata/base-internal-eth.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	amount := big.NewInt(1000000000000)
	r := Redemption{Grant: NewGrant(common.HexToAddress("0xecd14F7586f5053CECae148Ae26309C2277D86A8"), common.HexToAddress("0xa59C992Aa84657df4EbFF6F04E11a1632f6DB1FA"), amount, 1793677123, 1793680723, common.HexToHash("0x97619cdc9cc7e1aedf589b7aa6c60e2ef965343cfca4ce2963208c5cd0a3e4d6")), Amount: amount}
	if r.Digest().Hex() != "0xbf04a8b7b52626968b338d6466ea5d95e2acdce08dca8d5eb3187c65cb1c4652" {
		t.Fatal("wallet delegation digest")
	}
	const sig = "0xc247d6e80e245f3595aacc6a9085e55a73fbea84f75d11a475613d31ca969dc102660078f66425c7430807bc3563fa4d7c735ed30fc985abadda5a1e3a1bcfac1b"
	data, err := r.Data(sig)
	if err != nil || !bytes.Equal(data, f.Evidence.Transaction.Input) {
		t.Fatal("redemption calldata", err)
	}
	original, err := r.Signed(sig)
	if err != nil {
		t.Fatal(err)
	}
	log := f.Evidence.Receipt.Logs[1]
	data, err = original.EventData()
	if err != nil || log.Topics[0] != RedeemedTopic || !bytes.Equal(data, log.Data) {
		t.Fatal("redemption log", err)
	}
}
