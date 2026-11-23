package chainassets

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/shopspring/decimal"
)

const (
	// Largest submitted gas limit across the finalized EIP-3009 test transfers.
	baseUSDCReferenceGas = 125823
	// Type-2 unsigned envelope, empty access list, zero value and 292-byte
	// transferWithAuthorization calldata. See the RLP boundary test.
	baseUSDCUnsignedSize = 410
	// Delegation designation observed in the finalized Rabby and OKX tests.
	baseTestedDelegation = "0xef010063c0c19a282a1b52b07dd5a65b58948a07dae32b"
)

type BaseDepositFeeConfig struct{ FeePayer, Recipient string }

func newBaseDepositRoute(config BaseDepositFeeConfig) (*BaseDepositFeeConfig, error) {
	payer, err := NormalizeBaseAddress(config.FeePayer)
	if err != nil || common.HexToAddress(payer) == (common.Address{}) {
		return nil, fmt.Errorf("invalid Base USDC fee payer")
	}
	recipient, err := NormalizeBaseAddress(config.Recipient)
	if err != nil || common.HexToAddress(recipient) == (common.Address{}) {
		return nil, fmt.Errorf("invalid Base USDC recipient")
	}
	return &BaseDepositFeeConfig{FeePayer: payer, Recipient: recipient}, nil
}

type baseFeeBlock struct {
	Hash          common.Hash
	Number        hexutil.Uint64
	Timestamp     hexutil.Uint64
	BaseFeePerGas *hexutil.Big
}

func (b baseFeeBlock) expiresAt() time.Time {
	return time.Unix(int64(b.Timestamp), 0).Add(depositPreviewTTL)
}

type baseFeeHistory struct {
	OldestBlock   hexutil.Uint64
	BaseFeePerGas []*hexutil.Big
	Reward        [][]*hexutil.Big
}

func (h baseFeeHistory) executionEstimate(block baseFeeBlock) (*big.Int, error) {
	if h.OldestBlock != block.Number || len(h.BaseFeePerGas) != 2 || h.BaseFeePerGas[0] == nil || h.BaseFeePerGas[1] == nil || len(h.Reward) != 1 || len(h.Reward[0]) != 1 || h.Reward[0][0] == nil {
		return nil, invalid("incomplete Base fee history")
	}
	if h.BaseFeePerGas[0].ToInt().Cmp(block.BaseFeePerGas.ToInt()) != 0 || h.BaseFeePerGas[1].ToInt().Sign() == 0 {
		return nil, invalid("Base fee history differs from fee block or lacks next base fee")
	}
	price := new(big.Int).Add(h.BaseFeePerGas[1].ToInt(), h.Reward[0][0].ToInt())
	return price.Mul(price, big.NewInt(baseUSDCReferenceGas)), nil
}

type baseUSDCBudget struct {
	block                                 baseFeeBlock
	balance, execution, dataFee, operator *big.Int
	sponsorBalance                        hexutil.Big
}

func (s *Service) previewBaseUSDC(ctx context.Context, user int64, target, address string) (DepositPreview, error) {
	var empty DepositPreview
	route := s.quotes.baseUSDC
	if route == nil {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Base USDC wallet fee preview disabled"))
	}
	wallet, err := NormalizeBaseAddress(address)
	if err != nil || common.HexToAddress(wallet) == (common.Address{}) || wallet == route.Recipient || wallet == route.FeePayer {
		return empty, badTarget("invalid Base deposit source wallet")
	}
	start := time.Now()
	prices, err := s.depositPrices(ctx, baseusdc.AssetID, start)
	if err != nil {
		return empty, err
	}
	if _, err = s.depositQuote(baseusdc.AssetID, target, decimal.Zero, start, prices); err != nil {
		return empty, err
	}
	if s.limiter == nil {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("chain query limiter not configured"))
	}
	if err = s.limiter.Allow(ctx, user); err != nil {
		return empty, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	budget, err := s.baseUSDCEstimate(ctx, wallet)
	if err != nil {
		return empty, err
	}
	total := new(big.Int).Add(budget.execution, budget.dataFee)
	total.Add(total, budget.operator)
	if total.BitLen() > 256 {
		return empty, apperror.New(apperror.ServiceUnavailable, invalid("Base network fee overflow"))
	}
	if budget.sponsorBalance.ToInt().Cmp(total) < 0 {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Base fee payer cannot cover estimated network cost"))
	}
	entry := s.quotes.assets[baseusdc.AssetID]
	q, err := s.depositQuote(baseusdc.AssetID, target, decimal.NewFromBigInt(total, -int32(entry.native.Decimals)), start, prices)
	if err != nil {
		return empty, err
	}
	q.ExpiresAt = min(q.ExpiresAt, budget.block.expiresAt().UnixMilli())
	if time.Now().UnixMilli() >= q.ExpiresAt {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Base fee block or rate expired during estimate"))
	}
	gross, _ := decimal.NewFromString(q.PayQuantity)
	if gross.Shift(int32(entry.asset.Decimals)).BigInt().Cmp(budget.balance) > 0 {
		return empty, apperror.New(apperror.InsufficientBalance, nil)
	}
	q.WalletAddress, q.FeeBlockHash = address, budget.block.Hash.Hex()
	return q, nil
}

// The user has not yet authorized a token amount. Read prices and balances
// only; exact execution simulation requires the user's later signed calldata.
func (s *Service) baseUSDCEstimate(ctx context.Context, wallet string) (baseUSDCBudget, error) {
	var b baseUSDCBudget
	entry := s.quotes.assets[baseusdc.AssetID]
	endpoint := s.endpoints[BaseSepolia]
	if err := s.checkNetwork(ctx, entry.network, endpoint); err != nil {
		return b, apperror.New(apperror.ServiceUnavailable, err)
	}
	rpc := func(method string, params any, out any) error {
		if err := s.rpc(ctx, entry.network, endpoint, method, params, out); err != nil {
			return apperror.New(apperror.ServiceUnavailable, err)
		}
		return nil
	}
	if err := rpc("eth_getBlockByNumber", []any{"latest", false}, &b.block); err != nil {
		return b, err
	}
	now := time.Now()
	if b.block.Hash == (common.Hash{}) || b.block.Number == 0 || b.block.BaseFeePerGas == nil || uint64(b.block.Timestamp) > uint64(now.Unix()) || !b.block.expiresAt().After(now) {
		return b, apperror.New(apperror.ServiceUnavailable, invalid("invalid or stale Base fee block"))
	}
	tag := map[string]any{"blockHash": b.block.Hash.Hex(), "requireCanonical": true}
	var code hexutil.Bytes
	if err := rpc("eth_getCode", []any{wallet, tag}, &code); err != nil {
		return b, err
	}
	if len(code) != 0 && code.String() != baseTestedDelegation {
		return b, badTarget("unverified Base USDC signing account type")
	}
	if err := rpc("eth_getBalance", []any{s.quotes.baseUSDC.FeePayer, tag}, &b.sponsorBalance); err != nil {
		return b, err
	}
	var history baseFeeHistory
	if err := rpc("eth_feeHistory", []any{"0x1", b.block.Number.String(), []int{50}}, &history); err != nil {
		return b, err
	}
	var err error
	b.execution, err = history.executionEstimate(b.block)
	if err != nil {
		return b, apperror.New(apperror.ServiceUnavailable, err)
	}
	readWord := func(to, data string) (*big.Int, error) {
		var word common.Hash // Each ABI uint256 must occupy exactly 32 bytes.
		if err := rpc("eth_call", []any{map[string]string{"to": to, "data": data}, tag}, &word); err != nil {
			return nil, err
		}
		return word.Big(), nil
	}
	b.balance, err = readWord(entry.asset.Contract, "0x"+balanceOfSelector+strings.Repeat("0", 24)+wallet[2:])
	if err != nil {
		return b, err
	}
	b.dataFee, err = readWord(BaseGasPriceOracle, baseOracleCall("getL1FeeUpperBound(uint256)", big.NewInt(baseUSDCUnsignedSize)))
	if err != nil {
		return b, err
	}
	b.operator, err = readWord(BaseGasPriceOracle, baseOracleCall("getOperatorFee(uint256)", big.NewInt(baseUSDCReferenceGas)))
	return b, err
}

func baseOracleCall(signature string, argument *big.Int) string {
	return fmt.Sprintf("0x%x%064x", crypto.Keccak256([]byte(signature))[:4], argument)
}
