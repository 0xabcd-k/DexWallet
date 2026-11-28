// Package tronnative builds single owner-permission Nile TRX and TRC-20 transfers.
// The field numbers follow protocol.Transaction.raw, Contract, Any, TransferContract and TriggerSmartContract.
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
const triggerSmartContract = 31
const triggerTypeURL = "type.googleapis.com/protocol.TriggerSmartContract"
const transferSelector = "\xa9\x05\x9c\xbb"

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
	Sender, Recipient         string
	Sun                       int64
	Reference                 Head
	Expiration                int64
	TokenContract, TokenAtoms string
	FeeLimitSun               int64
}

func appendBytes(b []byte, field protowire.Number, v []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(b, field, protowire.BytesType), v)
}
func appendNumber(b []byte, field protowire.Number, v int64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(b, field, protowire.VarintType), uint64(v))
}

// TransferData encodes transfer(address,uint256), using the 20-byte TVM address.
func TransferData(recipient, atoms string) ([]byte, error) {
	to, err := AddressBytes(recipient)
	if err != nil {
		return nil, err
	}
	if len(atoms) == 0 || len(atoms) > 78 {
		return nil, errors.New("invalid TRC-20 amount")
	}
	n, ok := new(big.Int).SetString(atoms, 10)
	if !ok || n.Sign() <= 0 || n.BitLen() > 256 || n.String() != atoms {
		return nil, errors.New("noncanonical TRC-20 uint256 amount")
	}
	data := make([]byte, 4+32+32)
	copy(data, transferSelector)
	copy(data[16:36], to[1:])
	n.FillBytes(data[36:])
	return data, nil
}

func (t Transfer) contract(owner []byte) ([]byte, error) {
	value := appendBytes(nil, 1, owner)
	typeURL, kind := transferTypeURL, int64(transferContract)
	if t.TokenContract == "" {
		if t.Sun <= 0 || t.TokenAtoms != "" || t.FeeLimitSun != 0 {
			return nil, errors.New("invalid native TRX amounts")
		}
		to, err := AddressBytes(t.Recipient)
		if err != nil {
			return nil, err
		}
		value = appendBytes(value, 2, to)
		value = appendNumber(value, 3, t.Sun)
	} else {
		if t.Sun != 0 || t.FeeLimitSun <= 0 {
			return nil, errors.New("TRC-20 payout requires zero TRX and an explicit Energy fee limit")
		}
		token, err := AddressBytes(t.TokenContract)
		if err != nil {
			return nil, err
		}
		data, err := TransferData(t.Recipient, t.TokenAtoms)
		if err != nil {
			return nil, err
		}
		value = appendBytes(value, 2, token)
		value = appendBytes(value, 4, data)
		typeURL, kind = triggerTypeURL, triggerSmartContract
	}
	parameter := appendBytes(nil, 1, []byte(typeURL))
	parameter = appendBytes(parameter, 2, value)
	return appendBytes(appendNumber(nil, 1, kind), 2, parameter), nil
}

func (t Transfer) Raw() ([]byte, error) {
	if t.Sender == t.Recipient || !t.Reference.Valid() || t.Expiration <= t.Reference.Timestamp || t.Expiration-t.Reference.Timestamp > Lifetime.Milliseconds() {
		return nil, errors.New("invalid TRON recipient, reference or validity window")
	}
	owner, err := AddressBytes(t.Sender)
	if err != nil {
		return nil, err
	}
	contract, err := t.contract(owner)
	if err != nil {
		return nil, err
	}
	ref, _ := hex.DecodeString(t.Reference.ID)
	raw := appendBytes(nil, 1, ref[6:8])
	raw = appendBytes(raw, 4, ref[8:16])
	raw = appendNumber(raw, 8, t.Expiration)
	raw = appendBytes(raw, 11, contract)
	raw = appendNumber(raw, 14, t.Reference.Timestamp)
	if t.TokenContract != "" {
		raw = appendNumber(raw, 18, t.FeeLimitSun)
	}
	return raw, nil
}

// SingleSignatureBandwidth counts the protobuf transaction including the owner
// signature and the result reservation used by java-tron for these single-contract transfers.
func SingleSignatureBandwidth(raw []byte) int {
	const resultBytes = 64
	return int(protowire.SizeTag(1)) + protowire.SizeBytes(len(raw)) +
		int(protowire.SizeTag(2)) + protowire.SizeBytes(crypto.SignatureLength) + resultBytes
}

type Signed struct {
	tokenContract string
	hash          string
	wire          string
	expiration    int64
}

func (s Signed) ID() string                 { return s.hash }
func (s Signed) TokenContract() string      { return s.tokenContract }
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
		return Signed{}, errors.New("TRON authorization differs from original raw data or txID")
	}
	sig, err := VerifyOwner(t.Sender, sha256.Sum256(raw), signature)
	if err != nil {
		return Signed{}, err
	}
	wire := appendBytes(nil, 1, raw)
	return Signed{tokenContract: t.TokenContract, hash: txID, wire: hex.EncodeToString(appendBytes(wire, 2, sig)), expiration: t.Expiration}, nil
}

// VerifyOwner recovers the Nile owner for a transaction or a domain-separated off-chain digest.
func VerifyOwner(sender string, digest [32]byte, signature string) ([]byte, error) {
	if len(signature) != hex.EncodedLen(crypto.SignatureLength) {
		return nil, errors.New("invalid TRON signature size")
	}
	sig, err := hex.DecodeString(signature)
	if err != nil {
		return nil, errors.New("invalid TRON signature size")
	}
	recoverSig := bytes.Clone(sig)
	// TronWeb may encode the recovery ID as 27/28; preserve the original on the wire.
	if recoverSig[64] >= 27 {
		recoverSig[64] -= 27
	}
	if !crypto.ValidateSignatureValues(recoverSig[64], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:64]), true) {
		return nil, errors.New("invalid TRON signature values")
	}
	pub, err := crypto.SigToPub(digest[:], recoverSig)
	owner, errAddr := AddressBytes(sender)
	if err != nil || errAddr != nil || !bytes.Equal(crypto.PubkeyToAddress(*pub).Bytes(), owner[1:]) {
		return nil, errors.New("unexpected TRON signer")
	}
	return sig, nil
}
