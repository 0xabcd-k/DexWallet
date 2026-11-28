// Package baseeth encodes the single MetaMask 1.3.0 native ETH redemption
// supported by the Base Sepolia deposit route.
package baseeth

import (
	"bytes"
	"math/big"

	"github.com/0xabcd-k/DexWallet/backend/internal/evmsig"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const Manager = "0xdb9B1e94B5b69Df7e401DDbedE43491141047dB3"
const AssetID = "base-sepolia:eth"
const AccountCode = "0xef010063c0c19a282a1b52b07dd5a65b58948a07dae32b"
const caveatType = "Caveat(address enforcer,bytes terms)"
const delegationType = "Delegation(address delegate,address delegator,bytes32 authority,Caveat[] caveats,uint256 salt)" + caveatType

// MetaMask 1.3.0 enforcers for the fixed Base Sepolia permission.
const (
	periodAllowanceEnforcer = "0x9BC0FAf4Aca5AE429F4c06aEEaC517520CB16BD9"
	exactCalldataEnforcer   = "0x99F2e9bF15ce5eC84685604836F71aB835DBBdED"
	timestampEnforcer       = "0x1046bb45C8d673d4ea75321280DB34899413c069"
	allowedRedeemerEnforcer = "0xE144b0b2618071B4E56f746313528a669c7E65c5"
	allowedTargetEnforcer   = "0x7F20f61b1f09b08D970938F6fa563634d65c4EeB"
	revocationNonceEnforcer = "0xDE4f2FAC4B3D87A1d9953Ca5FC09FCa7F366254f"
)

// The manager receives opaque permission contexts. Each context is the ABI
// encoding of an array containing precisely one signed delegation.
var grantFields = []abi.ArgumentMarshaling{
	{Name: "delegate", Type: "address"}, {Name: "delegator", Type: "address"}, {Name: "authority", Type: "bytes32"},
	{Name: "caveats", Type: "tuple[]", Components: []abi.ArgumentMarshaling{
		{Name: "enforcer", Type: "address"}, {Name: "terms", Type: "bytes"}, {Name: "args", Type: "bytes"},
	}},
	{Name: "salt", Type: "uint256"}, {Name: "signature", Type: "bytes"},
}

func abiType(name string, fields ...abi.ArgumentMarshaling) abi.Type {
	t, err := abi.NewType(name, "", fields)
	if err != nil {
		panic(err)
	}
	return t
}

var grantList = abi.Arguments{{Type: abiType("tuple[]", grantFields...)}}
var redeemed = abi.NewEvent("RedeemedDelegation", "RedeemedDelegation", false, abi.Arguments{
	{Name: "rootDelegator", Type: abiType("address"), Indexed: true},
	{Name: "redeemer", Type: abiType("address"), Indexed: true},
	{Name: "delegation", Type: abiType("tuple", grantFields...)},
})
var redeem = abi.NewMethod("redeemDelegations", "redeemDelegations", abi.Function, "nonpayable", false, false, abi.Arguments{
	{Name: "permissionContexts", Type: abiType("bytes[]")}, {Name: "modes", Type: abiType("bytes32[]")}, {Name: "executionCallDatas", Type: abiType("bytes[]")},
}, nil)
var RedeemedTopic = redeemed.ID
var root = common.BytesToHash(bytes.Repeat([]byte{0xff}, 32))
var domain = crypto.Keccak256(hashText("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"), hashText("DelegationManager"), hashText("1"), word(big.NewInt(84532)), common.LeftPadBytes(common.HexToAddress(Manager).Bytes(), 32))

func hashText(s string) []byte { return crypto.Keccak256([]byte(s)) }
func word(n *big.Int) []byte   { return common.LeftPadBytes(n.Bytes(), 32) }

type Caveat struct {
	Enforcer    common.Address
	Terms, Args []byte
}
type Grant struct {
	Delegate, Delegator common.Address
	Authority           common.Hash
	Caveats             []Caveat
	Salt                *big.Int
	Signature           []byte
}

// NewGrant restricts the full lifetime allowance, calldata, expiry, redeemer
// and payee. The revocation nonce is zero; the order salt separates allowances.
func NewGrant(owner, payer common.Address, amount *big.Int, start, expiry int64, salt common.Hash) Grant {
	limit := bytes.Join([][]byte{word(amount), root.Bytes(), word(big.NewInt(start))}, nil)
	caveat := func(address string, terms []byte) Caveat {
		return Caveat{Enforcer: common.HexToAddress(address), Terms: terms}
	}
	return Grant{Delegate: payer, Delegator: owner, Authority: root, Salt: salt.Big(), Caveats: []Caveat{
		caveat(periodAllowanceEnforcer, limit),
		caveat(exactCalldataEnforcer, nil),
		caveat(timestampEnforcer, word(big.NewInt(expiry))),
		caveat(allowedRedeemerEnforcer, payer.Bytes()),
		caveat(allowedTargetEnforcer, payer.Bytes()),
		caveat(revocationNonceEnforcer, make([]byte, 32)),
	}}
}
func (g Grant) Digest() common.Hash {
	var hashes []byte
	for _, c := range g.Caveats {
		hashes = append(hashes, crypto.Keccak256(hashText(caveatType), common.LeftPadBytes(c.Enforcer.Bytes(), 32), crypto.Keccak256(c.Terms))...)
	}
	message := crypto.Keccak256(hashText(delegationType), common.LeftPadBytes(g.Delegate.Bytes(), 32), common.LeftPadBytes(g.Delegator.Bytes(), 32), g.Authority.Bytes(), crypto.Keccak256(hashes), word(g.Salt))
	return crypto.Keccak256Hash([]byte{0x19, 1}, domain, message)
}

func (g Grant) Signed(signature string) (Grant, error) {
	sig, err := evmsig.Verify(g.Digest(), g.Delegator, signature)
	g.Signature = sig
	return g, err
}

func (g Grant) Redemption(amount *big.Int) ([]byte, error) {
	ctx, err := grantList.Pack([]Grant{g})
	if err != nil {
		return nil, err
	}
	execution := append(g.Delegate.Bytes(), word(amount)...)
	args, err := redeem.Inputs.Pack([][]byte{ctx}, [][32]byte{{}}, [][]byte{execution})
	if err != nil {
		return nil, err
	}
	return append(bytes.Clone(redeem.ID), args...), nil
}

func (g Grant) EventData() ([]byte, error) {
	return redeemed.Inputs.NonIndexed().Pack(g)
}

type Redemption struct {
	Grant
	Amount *big.Int
}

func (r Redemption) Data(signature string) ([]byte, error) {
	g, err := r.Signed(signature)
	if err != nil {
		return nil, err
	}
	return g.Redemption(r.Amount)
}
