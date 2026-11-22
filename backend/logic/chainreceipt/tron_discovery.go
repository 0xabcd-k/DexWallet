package chainreceipt

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
)

type nileBlock struct {
	tronBlock
	Members []json.RawMessage `json:"transactions"`
}

func nileHistoryBlock(raw json.RawMessage) (nileBlock, error) {
	var b nileBlock
	if json.Unmarshal(raw, &b) != nil || !b.valid() || b.Header.Raw.Number <= 0 || b.Header.Raw.Timestamp <= 0 || !tronHex(b.Header.Raw.ParentHash, 32) {
		return b, bad("invalid Nile solid block header")
	}
	root := b.Header.Raw.TrieRoot
	if len(b.Members) == 0 && root != "" && root != strings.Repeat("0", 64) {
		return b, bad("missing transactions for nonempty Nile block")
	}
	return b, nil
}

type nileIncoming struct {
	req   TRONRequest
	proof chainassets.TRONEvidence
}

// Join by txID, since a bulk receipt response need not be in transaction order.
// Even receipts that do not produce a supported event must belong to this block.
func nileIncomingEvents(history chainassets.NileHistory, b nileBlock, asset chainassets.Asset) ([]nileIncoming, error) {
	if history.Infos == nil || len(history.Infos) != len(b.Members) {
		return nil, bad("incomplete Nile block receipt list")
	}
	infos := make(map[string]json.RawMessage, len(history.Infos))
	for _, raw := range history.Infos {
		var info struct {
			ID                          string
			BlockNumber, BlockTimeStamp tronCounter
		}
		if json.Unmarshal(raw, &info) != nil || !tronHex(info.ID, 32) || info.BlockNumber != b.Header.Raw.Number || info.BlockTimeStamp != b.Header.Raw.Timestamp || infos[info.ID] != nil {
			return nil, bad("duplicate or inconsistent Nile block receipt")
		}
		infos[info.ID] = raw
	}
	var events []nileIncoming
	for _, member := range b.Members {
		// Unrelated contracts have different parameter schemas.
		var summary struct {
			ID  string `json:"txID"`
			Raw struct {
				Contracts []struct{ Type string } `json:"contract"`
			} `json:"raw_data"`
		}
		if json.Unmarshal(member, &summary) != nil || len(summary.Raw.Contracts) == 0 {
			return nil, bad("incomplete Nile block transaction")
		}
		raw, ok := infos[summary.ID]
		if !ok {
			return nil, bad("Nile block transaction missing unique receipt")
		}
		delete(infos, summary.ID)
		if len(summary.Raw.Contracts) != 1 {
			continue
		}
		kind := summary.Raw.Contracts[0].Type
		if kind == "" {
			return nil, bad("missing Nile contract type")
		}
		if asset.Origin == chainassets.Native && kind != tronTransferContract || asset.Origin != chainassets.Native && kind != tronTriggerContract {
			continue
		}
		proof := chainassets.TRONEvidence{Genesis: history.Genesis, SolidHead: history.SolidHead, Block: history.Block, Transaction: member, Info: raw}
		req := TRONRequest{Network: chainassets.TRONNile, Hash: summary.ID, AssetID: asset.ID}
		if kind == tronTransferContract {
			var tx tronTransaction
			if json.Unmarshal(member, &tx) != nil {
				return nil, bad("invalid Nile TRX transfer")
			}
			value := tx.Raw.Contracts[0].Parameter.Value
			to, err := tronAddress(value.To)
			if err != nil {
				return nil, err
			}
			from, err := tronAddress(value.Owner)
			if err != nil {
				return nil, err
			}
			if !tronUserAddress(from) || from == to {
				continue
			}
			req.Recipient, req.Atoms = to, strconv.FormatInt(int64(value.Amount), 10)
			events = append(events, nileIncoming{req, proof})
			continue
		}
		var info struct {
			Logs []tronEventLog `json:"log"`
		}
		if json.Unmarshal(raw, &info) != nil {
			return nil, bad("invalid Nile receipt logs")
		}
		for i, log := range info.Logs {
			emitter, err := tronAddress("41" + log.Address)
			if err != nil {
				return nil, err
			}
			if emitter != asset.Contract || len(log.Topics) == 0 || !strings.EqualFold(log.Topics[0], ERC20TransferTopic[2:]) {
				continue
			}
			event, err := tronTokenEvent(log, i, asset)
			if err != nil {
				return nil, fmt.Errorf("Nile tx %s log %d: %w", summary.ID, i, err)
			}
			if !tronUserAddress(event.SourceAccount) || event.SourceAccount == event.RecipientAccount || event.Atoms == "0" {
				continue
			}
			req.Recipient, req.Atoms, req.LogIndex = event.RecipientAccount, event.Atoms, &i
			events = append(events, nileIncoming{req, proof})
		}
	}
	return events, nil
}
