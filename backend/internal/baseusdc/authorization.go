package baseusdc

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math/big"

	"github.com/0xabcd-k/DexWallet/backend/internal/evmsig"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const SepoliaChainID = 84532
const DomainName = "USDC"
const authorizationType = "TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)"
const OrderNoncePrefix = "dexwallet:deposit:base-sepolia:"

var orderTag = crypto.Keccak256([]byte(OrderNoncePrefix))[:16]
var AuthorizationUsedTopic = crypto.Keccak256Hash([]byte("AuthorizationUsed(address,bytes32)"))

func OrderNonce(idx string) (common.Hash, error) {
	var nonce common.Hash
	id, err := hex.DecodeString(idx)
	if err != nil || len(id) != 16 {
		return nonce, errors.New("invalid Base deposit order ID")
	}
	copy(nonce[:16], orderTag)
	copy(nonce[16:], id)
	return nonce, nil
}

func DepositIdx(nonce common.Hash) (string, bool) {
	if !bytes.Equal(nonce[:16], orderTag) {
		return "", false
	}
	return hex.EncodeToString(nonce[16:]), true
}

type Authorization struct {
	From, To    common.Address
	Amount      *big.Int
	ValidBefore int64
	Nonce       common.Hash
}

func domainWord(s string) []byte { return crypto.Keccak256([]byte(s)) }
func (a Authorization) words() []byte {
	return bytes.Join([][]byte{common.LeftPadBytes(a.From.Bytes(), 32), common.LeftPadBytes(a.To.Bytes(), 32), common.LeftPadBytes(a.Amount.Bytes(), 32), make([]byte, 32), common.LeftPadBytes(big.NewInt(a.ValidBefore).Bytes(), 32), a.Nonce.Bytes()}, nil)
}
func (a Authorization) Digest() common.Hash {
	domain := crypto.Keccak256(domainWord("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"), domainWord(DomainName), domainWord("2"), common.LeftPadBytes(big.NewInt(SepoliaChainID).Bytes(), 32), common.LeftPadBytes(common.HexToAddress(SepoliaContract).Bytes(), 32))
	return crypto.Keccak256Hash([]byte{0x19, 0x01}, domain, crypto.Keccak256(domainWord(authorizationType), a.words()))
}

// Data accepts the canonical low-s wallet signature with Ethereum v=27/28.
func (a Authorization) Data(signature string) ([]byte, error) {
	sig, err := evmsig.Verify(a.Digest(), a.From, signature)
	if err != nil {
		return nil, err
	}
	v := sig[64]
	data := append(domainWord("transferWithAuthorization(address,address,uint256,uint256,uint256,bytes32,uint8,bytes32,bytes32)")[:4], a.words()...)
	data = append(data, common.LeftPadBytes([]byte{v}, 32)...)
	return append(data, sig[:64]...), nil
}
