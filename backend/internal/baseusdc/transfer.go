// Package baseusdc handles the single direct ERC-20 transfer accepted for Sepolia payouts.
package baseusdc

import (
	"bytes"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const (
	SepoliaContract  = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"
	AssetID          = "base-sepolia:usdc"
	transferSelector = "\xa9\x05\x9c\xbb"
	addressOffset    = 4 + 12
	amountOffset     = 4 + 32
	transferSize     = amountOffset + 32
)

// TransferData encodes transfer(address,uint256) for an already validated uint256.
func TransferData(recipient common.Address, amount *big.Int) []byte {
	data := make([]byte, transferSize)
	copy(data, transferSelector)
	copy(data[addressOffset:amountOffset], recipient[:])
	amount.FillBytes(data[amountOffset:])
	return data
}

func ParseTransfer(tx *types.Transaction) (common.Address, *big.Int, bool) {
	if tx == nil || tx.To() == nil || *tx.To() != common.HexToAddress(SepoliaContract) || tx.Value().Sign() != 0 {
		return common.Address{}, nil, false
	}
	data := tx.Data()
	var padding [12]byte
	if len(data) != transferSize || string(data[:4]) != transferSelector || !bytes.Equal(data[4:addressOffset], padding[:]) {
		return common.Address{}, nil, false
	}
	recipient := common.BytesToAddress(data[addressOffset:amountOffset])
	amount := new(big.Int).SetBytes(data[amountOffset:])
	return recipient, amount, recipient != (common.Address{}) && amount.Sign() > 0
}
