// Package tronnative builds a single owner-permission TRX TransferContract.
// The field numbers follow protocol.Transaction.raw, Contract, Any and TransferContract.
package tronnative

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/mr-tron/base58"
	"google.golang.org/protobuf/encoding/protowire"
)

const Lifetime = 10 * time.Minute
const transferTypeURL = "type.googleapis.com/protocol.TransferContract"
const transferContract = 1

type Head struct {
	ID        string
	Height    int64
	Timestamp int64
}

func (h Head) Valid() bool {
	id, err := hex.DecodeString(h.ID)
	return err == nil && len(id) == 32 && h.Height > 0 && binary.BigEndian.Uint64(id[:8]) == uint64(h.Height) && h.Timestamp > 0
}

func AddressBytes(s string) ([]byte, error) {
	if len(s) != 34 {
		return nil, errors.New("invalid TRON address length")
	}
	b, err := base58.Decode(s)
	if err != nil || len(b) != 25 || b[0] != 0x41 || bytes.Equal(b[1:21], make([]byte, 20)) {
		return nil, errors.New("invalid TRON payment address")
	}
	first := sha256.Sum256(b[:21])
	second := sha256.Sum256(first[:])
	if !bytes.Equal(b[21:], second[:4]) {
		return nil, errors.New("invalid TRON address checksum")
	}
	return b[:21], nil
}

type Transfer struct {
	Sender, Recipient string
	Sun               int64
	Reference         Head
	Expiration        int64
}

func appendBytes(b []byte, field protowire.Number, v []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(b, field, protowire.BytesType), v)
}
func appendNumber(b []byte, field protowire.Number, v int64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(b, field, protowire.VarintType), uint64(v))
}
func (t Transfer) Raw() ([]byte, error) {
	if t.Sun <= 0 || t.Sender == t.Recipient || !t.Reference.Valid() || t.Expiration <= t.Reference.Timestamp || t.Expiration-t.Reference.Timestamp > Lifetime.Milliseconds() {
		return nil, errors.New("invalid TRX amount, reference or validity window")
	}
	owner, err := AddressBytes(t.Sender)
	if err != nil {
		return nil, err
	}
	to, err := AddressBytes(t.Recipient)
	if err != nil {
		return nil, err
	}
	ref, _ := hex.DecodeString(t.Reference.ID)
	value := appendBytes(nil, 1, owner)
	value = appendBytes(value, 2, to)
	value = appendNumber(value, 3, t.Sun)
	parameter := appendBytes(nil, 1, []byte(transferTypeURL))
	parameter = appendBytes(parameter, 2, value)
	contract := appendNumber(nil, 1, transferContract)
	contract = appendBytes(contract, 2, parameter)
	raw := appendBytes(nil, 1, ref[6:8])
	raw = appendBytes(raw, 4, ref[8:16])
	raw = appendNumber(raw, 8, t.Expiration)
	raw = appendBytes(raw, 11, contract)
	return appendNumber(raw, 14, t.Reference.Timestamp), nil
}

type Signed struct {
	hash       string
	wire       string
	expiration int64
}

func (s Signed) ID() string                 { return s.hash }
func (s Signed) Hex() string                { return s.wire }
func (s Signed) Expired(now time.Time) bool { return now.UnixMilli() >= s.expiration }
func TxID(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// Bind rebuilds all signed bytes, verifies that the local authorization matches,
// then recovers the platform owner from the one TronWeb-compatible signature.
func (t Transfer) Bind(rawHex, txID, signature string) (Signed, error) {
	raw, err := t.Raw()
	if err != nil || hex.EncodeToString(raw) != rawHex || TxID(raw) != txID {
		return Signed{}, errors.New("TRX authorization differs from original raw data or txID")
	}
	if len(signature) != hex.EncodedLen(crypto.SignatureLength) {
		return Signed{}, errors.New("invalid TRX signature size")
	}
	sig, err := hex.DecodeString(signature)
	if err != nil {
		return Signed{}, errors.New("invalid TRX signature size")
	}
	recoverSig := bytes.Clone(sig)
	// TronWeb may encode the recovery ID as 27/28; preserve the original on the wire.
	if recoverSig[64] >= 27 {
		recoverSig[64] -= 27
	}
	if !crypto.ValidateSignatureValues(recoverSig[64], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:64]), true) {
		return Signed{}, errors.New("invalid TRX signature values")
	}
	digest := sha256.Sum256(raw)
	pub, err := crypto.SigToPub(digest[:], recoverSig)
	owner, _ := AddressBytes(t.Sender)
	if err != nil || !bytes.Equal(crypto.PubkeyToAddress(*pub).Bytes(), owner[1:]) {
		return Signed{}, errors.New("TRX signature does not authorize platform sender")
	}
	wire := appendBytes(nil, 1, raw)
	return Signed{hash: txID, wire: hex.EncodeToString(appendBytes(wire, 2, sig)), expiration: t.Expiration}, nil
}
