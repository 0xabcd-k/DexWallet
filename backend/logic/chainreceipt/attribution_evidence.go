package chainreceipt

import (
	"bytes"
	"encoding/json"
	"regexp"

	"github.com/0xabcd-k/DexWallet/backend/logic/chainassets"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

const receiveMemoPrefix = "dw:"

var receiveMemo = regexp.MustCompile(`^dw:[0-9a-f]{32}$`)

// Read inclusion time from the original finalized evidence, not from the time it was indexed.
// The four stored evidence formats are immutable and predate collection requests.
func receiveEvidence(k EventKey, raw, route string) (int64, string, error) {
	var record struct{ Transaction, Block, Info, Parent json.RawMessage }
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return 0, "", bad("invalid stored receipt evidence")
	}
	var when int64
	switch k.Network {
	case chainassets.BaseSepolia:
		var block struct{ Timestamp string }
		if err := json.Unmarshal(record.Block, &block); err != nil {
			return 0, "", bad("invalid Base inclusion block")
		}
		n, err := quantity(block.Timestamp)
		if err != nil || !n.IsInt64() {
			return 0, "", bad("invalid Base block timestamp")
		}
		when = n.Int64()
	case chainassets.SolanaDevnet:
		var tx struct{ BlockTime int64 }
		if err := json.Unmarshal(record.Transaction, &tx); err != nil {
			return 0, "", bad("invalid Solana block time")
		}
		when = tx.BlockTime
	case chainassets.TRONNile:
		var info struct{ BlockTimeStamp int64 }
		if err := json.Unmarshal(record.Info, &info); err != nil {
			return 0, "", bad("invalid TRON inclusion time")
		}
		when = info.BlockTimeStamp / 1000
	case chainassets.TONTestnet:
		tx, err := tonOneTransaction(record.Transaction)
		if err != nil {
			return 0, "", err
		}
		when = tx.Now
		if route == TONMemo && when > 0 {
			ref, err := tonReceiveReference(k.Kind, tx, record.Parent)
			return when, ref, err
		}
	}
	if when <= 0 {
		return 0, "", bad("missing confirmed execution time")
	}
	return when, "", nil
}

func tonReceiveReference(kind string, tx tonTransaction, parentRaw json.RawMessage) (string, error) {
	body, err := tonBody(tx.In)
	if err != nil {
		return "", err
	}
	if kind == TONJetton {
		var received tonInternalTransfer
		if err = tonPayload(body, &received); err != nil {
			return "", err
		}
		parent, err := tonOneTransaction(parentRaw)
		if err != nil {
			return "", err
		}
		original, err := tonBody(parent.In)
		if err != nil {
			return "", err
		}
		var sent jetton.TransferPayload
		if err = tonPayload(original, &sent); err != nil {
			return "", err
		}
		if sent.ForwardPayload == nil || received.Payload == nil || !bytes.Equal(sent.ForwardPayload.Hash(), received.Payload.Hash()) {
			return "", bad("Jetton forwarded reference differs from payer message")
		}
		body = received.Payload
	}
	return tonPaymentComment(body)
}

func tonPaymentComment(body *cell.Cell) (string, error) {
	s := body.BeginParse()
	op, err := s.LoadUInt(32)
	if err != nil || op != 0 {
		return "", ErrNoReceiveRequest
	}
	const length = len(receiveMemoPrefix) + 32
	note := make([]byte, 0, length)
	for {
		bits := s.BitsLeft()
		if s.IsSpecial() || bits%8 != 0 || bits > uint((length-len(note))*8) || s.RefsNum() > 1 {
			return "", ErrNoReceiveRequest
		}
		b, err := s.LoadSlice(bits)
		if err != nil {
			return "", ErrNoReceiveRequest
		}
		note = append(note, b...)
		if s.RefsNum() == 0 {
			break
		}
		if s, err = s.LoadRef(); err != nil {
			return "", ErrNoReceiveRequest
		}
	}
	if !receiveMemo.Match(note) {
		return "", ErrNoReceiveRequest
	}
	return string(note), nil
}
