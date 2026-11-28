// Package evmsig checks wallet signatures over an already domain-separated digest.
package evmsig

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// Verify returns the 65-byte, low-s Ethereum signature with its original v (27 or 28).
func Verify(digest common.Hash, owner common.Address, encoded string) ([]byte, error) {
	sig, err := hexutil.Decode(encoded)
	if err != nil || len(sig) != 65 || (sig[64] != 27 && sig[64] != 28) {
		return nil, errors.New("invalid wallet signature encoding")
	}
	sig[64] -= 27
	if !crypto.ValidateSignatureValues(sig[64], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:64]), true) {
		return nil, errors.New("noncanonical wallet signature")
	}
	pub, err := crypto.SigToPub(digest.Bytes(), sig)
	if err != nil || crypto.PubkeyToAddress(*pub) != owner {
		return nil, errors.New("unexpected wallet signer")
	}
	sig[64] += 27
	return sig, nil
}
