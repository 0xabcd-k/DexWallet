// Package solanaspl describes the two legacy SPL payout messages supported on Devnet.
package solanaspl

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	"filippo.io/edwards25519"
	"github.com/mr-tron/base58"
)

const (
	Program           = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	AssociatedProgram = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"
	SystemProgram     = "11111111111111111111111111111111"
	Decimals          = 6
	transferChecked   = 12
	createIdempotent  = 1
)

type Transfer struct {
	Sender, Recipient, Mint, SourceATA, DestinationATA string
	Amount                                             uint64
	CreateATA                                          bool
}

func publicKey(s string) ([]byte, error) {
	if len(s) > 44 {
		return nil, errors.New("invalid Solana public key length")
	}
	b, err := base58.Decode(s)
	if err != nil || len(b) != 32 {
		return nil, errors.New("invalid Solana public key")
	}
	return b, nil
}
func associatedAddress(owner, mint string) (string, error) {
	o, err := publicKey(owner)
	if err != nil {
		return "", err
	}
	m, err := publicKey(mint)
	if err != nil {
		return "", err
	}
	token, _ := publicKey(Program)
	ata, _ := publicKey(AssociatedProgram)
	for bump := 255; bump >= 0; bump-- {
		h := sha256.New()
		for _, seed := range [][]byte{o, token, m, {byte(bump)}, ata, []byte("ProgramDerivedAddress")} {
			h.Write(seed)
		}
		key := h.Sum(nil)
		if _, err := new(edwards25519.Point).SetBytes(key); err != nil {
			return base58.Encode(key), nil
		}
	}
	return "", errors.New("cannot derive associated token account")
}
func New(sender, recipient, mint, atoms string, create bool) (Transfer, error) {
	n, err := strconv.ParseUint(atoms, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != atoms {
		return Transfer{}, errors.New("positive canonical SPL amount required")
	}
	source, err := associatedAddress(sender, mint)
	if err != nil {
		return Transfer{}, err
	}
	dest, err := associatedAddress(recipient, mint)
	if err != nil {
		return Transfer{}, err
	}
	t := Transfer{sender, recipient, mint, source, dest, n, create}
	seen := map[string]bool{}
	for _, key := range []string{sender, recipient, mint, source, dest, SystemProgram, Program, AssociatedProgram} {
		if seen[key] {
			return Transfer{}, fmt.Errorf("overlapping SPL account %s", key)
		}
		seen[key] = true
	}
	return t, nil
}

// Fee payer first, then both writable ATAs, then read-only accounts in instruction order.
func (t Transfer) Keys() []string {
	if t.CreateATA {
		return []string{t.Sender, t.DestinationATA, t.SourceATA, AssociatedProgram, t.Recipient, t.Mint, SystemProgram, Program}
	}
	return []string{t.Sender, t.SourceATA, t.DestinationATA, Program, t.Mint}
}
func (t Transfer) TransferIndex() int {
	if t.CreateATA {
		return 1
	}
	return 0
}
func (t Transfer) Message(blockhash string) ([]byte, error) {
	hash, err := publicKey(blockhash)
	if err != nil {
		return nil, err
	}
	keys := t.Keys()
	msg := []byte{1, 0, byte(len(keys) - 3), byte(len(keys))}
	for _, k := range keys {
		b, err := publicKey(k)
		if err != nil {
			return nil, err
		}
		msg = append(msg, b...)
	}
	msg = append(msg, hash...)
	// In each compiled instruction Solana uses one-byte indices into the
	// static account list above. All counts fit a single shortvec byte.
	indices := make(map[string]byte, len(keys))
	for i, key := range keys {
		indices[key] = byte(i)
	}
	appendInstruction := func(program string, accounts []string, data []byte) {
		msg = append(msg, indices[program], byte(len(accounts)))
		for _, account := range accounts {
			msg = append(msg, indices[account])
		}
		msg = append(msg, byte(len(data)))
		msg = append(msg, data...)
	}
	msg = append(msg, byte(1+t.TransferIndex()))
	if t.CreateATA {
		appendInstruction(AssociatedProgram, []string{t.Sender, t.DestinationATA, t.Recipient, t.Mint, SystemProgram, Program}, []byte{createIdempotent})
	}
	data := binary.LittleEndian.AppendUint64([]byte{transferChecked}, t.Amount)
	appendInstruction(Program, []string{t.SourceATA, t.Mint, t.DestinationATA, t.Sender}, append(data, Decimals))
	return msg, nil
}
