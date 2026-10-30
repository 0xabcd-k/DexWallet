// Package idcodec converts internal positive IDs to public idx strings.
// idx is a representation, never an authorization token.
package idcodec

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/capitalone/fpe/ff1"
)

type Kind string

const (
	User           Kind = "UserId"
	Admin          Kind = "AdminId"
	Merchant       Kind = "MerchantId"
	LedgerEntry    Kind = "LedgerEntryId"
	LedgerVoucher  Kind = "LedgerVoucherId"
	Transfer       Kind = "TransferId"
	Event          Kind = "EventId"
	BackgroundTask Kind = "BackgroundTaskId"
	Deposit        Kind = "DepositId"
	RewardGrant    Kind = "RewardGrantId"
	AccountHold    Kind = "AccountHoldId"
	Withdrawal     Kind = "WithdrawalId"
	Payment        Kind = "PaymentId"
)
const minDigits = 16
const maxDigits = 19

var ErrInvalid = errors.New("invalid idx or id")

type Codec struct {
	key  string
	kind Kind
}

func New(key string, kind Kind) (*Codec, error) {
	if kind == "" {
		return nil, errors.New("idx kind is required")
	}
	if _, err := ff1.NewCipher(10, len(kind), []byte(key), []byte(kind)); err != nil {
		return nil, fmt.Errorf("idx configuration: %w", err)
	}
	return &Codec{key: key, kind: kind}, nil
}

// A fresh FF1 cipher per call avoids sharing the library's mutable CBC state.
func (c *Codec) cipher() (ff1.Cipher, error) {
	return ff1.NewCipher(10, len(c.kind), []byte(c.key), []byte(c.kind))
}
func (c *Codec) Encode(id int64) (string, error) {
	if id <= 0 {
		return "", ErrInvalid
	}
	cipher, err := c.cipher()
	if err != nil {
		return "", err
	}
	encoded, err := cipher.Encrypt(fmt.Sprintf("%016d", id))
	if err != nil {
		return "", err
	}
	return "0" + encoded, nil
}
func (c *Codec) Decode(idx string) (int64, error) {
	if len(idx) < minDigits+1 || len(idx) > maxDigits+1 || idx[0] != '0' {
		return 0, ErrInvalid
	}
	for _, b := range []byte(idx[1:]) {
		if b < '0' || b > '9' {
			return 0, ErrInvalid
		}
	}
	cipher, err := c.cipher()
	if err != nil {
		return 0, err
	}
	plain, err := cipher.Decrypt(idx[1:])
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	id, err := strconv.ParseInt(plain, 10, 64)
	if err != nil || id <= 0 || fmt.Sprintf("%016d", id) != plain {
		return 0, ErrInvalid
	}
	return id, nil
}
