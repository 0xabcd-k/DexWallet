package chainassets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/baseeth"
	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/shopspring/decimal"
)

type BaseProof interface {
	Digest() common.Hash
	Data(string) ([]byte, error)
}
type BaseDepositAuthorization struct {
	From, To common.Address
	Amount   *big.Int
	BaseProof
}
type baseExecutionRoute struct {
	contract     string
	gasLimit     uint64
	unsignedSize int64
	decimals     int32
}

func baseExecutionTerms(asset string) (baseExecutionRoute, error) {
	switch asset {
	case baseusdc.AssetID:
		return baseExecutionRoute{baseusdc.SepoliaContract, baseUSDCReferenceGas, baseUSDCUnsignedSize, 6}, nil
	case BaseSepoliaETH:
		return baseExecutionRoute{baseeth.Manager, baseETHReferenceGas, baseETHUnsignedSize, 18}, nil
	default:
		return baseExecutionRoute{}, errors.New("unsupported Base deposit asset")
	}
}
func (q DepositPreview) BaseAuthorization() (BaseDepositAuthorization, error) {
	var a BaseDepositAuthorization
	route, err := baseExecutionTerms(q.AssetID)
	if err != nil {
		return a, err
	}
	if len(q.QuoteIdx) != 32 || q.GasAssetID != BaseSepoliaETH || !q.WalletFeePreview || q.BaseValidBefore <= q.ExpiresAt/1000 || q.MaxGas != q.EstimatedGas {
		return a, errors.New("confirmed Base authorization required")
	}
	nonce, err := baseusdc.OrderNonce(q.QuoteIdx)
	if err != nil {
		return a, err
	}
	for _, v := range []string{q.WalletAddress, q.FeePayer, q.Recipient} {
		if _, err := NormalizeBaseAddress(v); err != nil || common.HexToAddress(v) == (common.Address{}) {
			return a, errors.New("invalid Base deposit route")
		}
	}
	if !rateSyntax.MatchString(q.PayQuantity) {
		return a, errors.New("invalid Base deposit amount")
	}
	gross, _ := decimal.NewFromString(q.PayQuantity)
	atoms := gross.Shift(route.decimals)
	if !atoms.IsInteger() || !atoms.IsPositive() || atoms.BigInt().BitLen() > 256 {
		return a, errors.New("invalid Base deposit precision")
	}
	a = BaseDepositAuthorization{From: common.HexToAddress(q.WalletAddress), To: common.HexToAddress(q.Recipient), Amount: atoms.BigInt()}
	if q.AssetID == BaseSepoliaETH {
		if a.To != common.HexToAddress(q.FeePayer) || q.QuotedAt <= 0 || q.QuotedAt >= q.ExpiresAt {
			return a, errors.New("invalid native ETH grant terms")
		}
		a.BaseProof = baseeth.Redemption{Grant: baseeth.NewGrant(a.From, a.To, a.Amount, q.QuotedAt/1000, q.BaseValidBefore, nonce), Amount: a.Amount}
	} else {
		a.BaseProof = baseusdc.Authorization{From: a.From, To: a.To, Amount: a.Amount, ValidBefore: q.BaseValidBefore, Nonce: nonce}
	}
	if a.From == a.To || a.From == common.HexToAddress(q.FeePayer) {
		return a, errors.New("overlapping deposit accounts")
	}
	return a, nil
}

func (s *Service) AdmitBaseDeposit(ctx context.Context, user int64, q DepositPreview, data []byte) error {
	route := s.quotes.base[q.AssetID]
	if route == nil || s.quotes.baseExecutionWindow(q.AssetID) == 0 || q.Recipient != route.Recipient || q.FeePayer != route.FeePayer {
		return apperror.New(apperror.Unprocessable, errors.New("Base authorization disabled for this route"))
	}
	if s.limiter == nil {
		return apperror.New(apperror.ServiceUnavailable, errors.New("missing chain limiter"))
	}
	if err := s.limiter.Allow(ctx, user); err != nil {
		return err
	}
	if _, err := s.BaseDepositEnvelope(ctx, q, data); err != nil {
		return err
	}
	return nil
}

// Build a zero-value, type-2 call after simulating the exact signed authorization.
// Cap the execution gas price inside the confirmed total after L1 and operator reserves.
func (s *Service) BaseDepositEnvelope(ctx context.Context, q DepositPreview, data []byte) (*types.Transaction, error) {
	route, err := baseExecutionTerms(q.AssetID)
	if err != nil {
		return nil, err
	}
	if time.Now().Unix() >= q.BaseValidBefore {
		return nil, apperror.New(apperror.Conflict, errors.New("Base authorization expired"))
	}
	if err := s.checkNativeDepositAccounts(ctx, q, "pending"); err != nil {
		return nil, err
	}
	call := map[string]string{"from": q.FeePayer, "to": route.contract, "value": "0x0", "data": hexutil.Encode(data)}
	var result hexutil.Bytes
	if err := s.baseSepoliaRPC(ctx, "eth_call", []any{call, "pending"}, &result); err != nil {
		return nil, fmt.Errorf("simulate Base authorization: %w", err)
	}
	if len(result) != 0 {
		return nil, errors.New("unexpected redemption result")
	}
	var gas hexutil.Uint64
	if err := s.baseSepoliaRPC(ctx, "eth_estimateGas", []any{call}, &gas); err != nil {
		return nil, err
	}
	if gas == 0 || uint64(gas) > route.gasLimit {
		return nil, apperror.New(apperror.Conflict, errors.New("signed call exceeds quoted gas allowance"))
	}
	limit := min(uint64(route.gasLimit), uint64(gas)+uint64(gas)/10)
	var block baseFeeBlock
	if err := s.baseSepoliaRPC(ctx, "eth_getBlockByNumber", []any{"latest", false}, &block); err != nil {
		return nil, err
	}
	if block.BaseFeePerGas == nil || block.Hash == (common.Hash{}) || !block.expiresAt().After(time.Now()) {
		return nil, errors.New("invalid fee block")
	}
	tag := map[string]any{"blockHash": block.Hash.Hex(), "requireCanonical": true}
	reserve := func(method string, arg int64) (*big.Int, error) {
		var word common.Hash
		err := s.baseSepoliaRPC(ctx, "eth_call", []any{map[string]string{"to": BaseGasPriceOracle, "data": baseOracleCall(method, big.NewInt(arg))}, tag}, &word)
		return word.Big(), err
	}
	l1, err := reserve("getL1FeeUpperBound(uint256)", route.unsignedSize)
	if err != nil {
		return nil, err
	}
	op, err := reserve("getOperatorFee(uint256)", int64(limit))
	if err != nil {
		return nil, err
	}
	maxCost, err := q.BaseFeeLimitWei()
	if err != nil {
		return nil, err
	}
	remaining := new(big.Int).Sub(maxCost, new(big.Int).Add(l1, op))
	price := new(big.Int).Quo(remaining, new(big.Int).SetUint64(limit))
	var hist baseFeeHistory
	if err = s.baseSepoliaRPC(ctx, "eth_feeHistory", []any{"0x1", block.Number.String(), []int{50}}, &hist); err != nil {
		return nil, err
	}
	needed, err := hist.executionEstimate(block, 1)
	if err != nil {
		return nil, err
	}
	if price.Cmp(needed) < 0 {
		return nil, apperror.New(apperror.Conflict, errors.New("current fee exceeds original Base quote"))
	}
	// Leave room for L1 fluctuations while keeping the original bound.
	preferred := new(big.Int).Add(needed, new(big.Int).Div(new(big.Int).Set(needed), big.NewInt(4)))
	if preferred.Cmp(price) < 0 {
		price = preferred
	}
	nonce, err := s.PendingBaseNonce(ctx, q.FeePayer)
	if err != nil {
		return nil, err
	}
	contract := common.HexToAddress(route.contract)
	return types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(BaseSepoliaChainID), Nonce: nonce, Gas: limit, GasTipCap: hist.Reward[0][0].ToInt(), GasFeeCap: price, To: &contract, Data: data}), nil
}
func (q DepositPreview) BaseFeeLimitWei() (*big.Int, error) {
	if !rateSyntax.MatchString(q.MaxGas) {
		return nil, errors.New("invalid Base fee cap")
	}
	d, _ := decimal.NewFromString(q.MaxGas)
	w := d.Shift(18)
	if !w.IsInteger() || !w.IsPositive() {
		return nil, errors.New("invalid Base fee precision")
	}
	return w.BigInt(), nil
}
func (s *Service) BroadcastBaseDeposit(ctx context.Context, tx *types.Transaction, q DepositPreview, signature string) error {
	route, err := baseExecutionTerms(q.AssetID)
	if err != nil {
		return err
	}
	a, err := q.BaseAuthorization()
	if err != nil {
		return err
	}
	data, err := a.Data(signature)
	if err != nil {
		return err
	}
	if err := CheckBaseDepositWire(q, tx, data); err != nil {
		return err
	}
	// Recheck the original envelope's total budget immediately before each broadcast.
	if time.Now().Unix() >= q.BaseValidBefore {
		return errors.New("Base authorization expired")
	}
	var block baseFeeBlock
	if err := s.baseSepoliaRPC(ctx, "eth_getBlockByNumber", []any{"latest", false}, &block); err != nil {
		return err
	}
	if block.Hash == (common.Hash{}) || block.BaseFeePerGas == nil || !block.expiresAt().After(time.Now()) {
		return errors.New("invalid Base preflight block")
	}
	tag := map[string]any{"blockHash": block.Hash.Hex(), "requireCanonical": true}
	total := new(big.Int).Mul(tx.GasFeeCap(), new(big.Int).SetUint64(tx.Gas()))
	for _, item := range []struct {
		method string
		arg    int64
	}{{"getL1FeeUpperBound(uint256)", route.unsignedSize}, {"getOperatorFee(uint256)", int64(tx.Gas())}} {
		var word common.Hash
		if err := s.baseSepoliaRPC(ctx, "eth_call", []any{map[string]string{"to": BaseGasPriceOracle, "data": baseOracleCall(item.method, big.NewInt(item.arg))}, tag}, &word); err != nil {
			return err
		}
		total.Add(total, word.Big())
	}
	cap, err := q.BaseFeeLimitWei()
	if err != nil {
		return err
	}
	if total.Cmp(cap) > 0 || tx.GasFeeCap().Cmp(block.BaseFeePerGas.ToInt()) < 0 {
		return apperror.New(apperror.Conflict, errors.New("original signed envelope no longer fits budget"))
	}
	if err := s.checkNativeDepositAccounts(ctx, q, tag); err != nil {
		return err
	}
	var res hexutil.Bytes
	if err := s.baseSepoliaRPC(ctx, "eth_call", []any{map[string]string{"from": q.FeePayer, "to": route.contract, "gas": hexutil.EncodeUint64(tx.Gas()), "data": hexutil.Encode(data)}, tag}, &res); err != nil {
		return err
	}
	if len(res) != 0 {
		return errors.New("unexpected preflight result")
	}
	return s.submitBase(ctx, tx)
}
func CheckBaseDepositWire(q DepositPreview, tx *types.Transaction, data []byte) error {
	route, err := baseExecutionTerms(q.AssetID)
	if err != nil {
		return err
	}
	if tx == nil || tx.Type() != types.DynamicFeeTxType || tx.ChainId().Cmp(big.NewInt(BaseSepoliaChainID)) != 0 || tx.To() == nil || *tx.To() != common.HexToAddress(route.contract) || tx.Value().Sign() != 0 || len(tx.AccessList()) != 0 || !bytes.Equal(tx.Data(), data) {
		return errors.New("unexpected Base deposit call")
	}
	cap, err := q.BaseFeeLimitWei()
	if err != nil {
		return err
	}
	if tx.Gas() == 0 || tx.Gas() > route.gasLimit || tx.GasFeeCap().Sign() <= 0 || tx.GasTipCap().Cmp(tx.GasFeeCap()) > 0 || new(big.Int).Mul(tx.GasFeeCap(), new(big.Int).SetUint64(tx.Gas())).Cmp(cap) > 0 {
		return errors.New("signed Base fee exceeds original budget")
	}
	payer, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil || payer != common.HexToAddress(q.FeePayer) {
		return errors.New("unexpected Base fee payer")
	}
	return nil
}

// A delegation or an EOA payee can change after the quote was issued.
func (s *Service) checkNativeDepositAccounts(ctx context.Context, q DepositPreview, tag any) error {
	if q.AssetID != BaseSepoliaETH {
		return nil
	}
	for _, account := range []struct{ address, code string }{{q.WalletAddress, baseeth.AccountCode}, {q.Recipient, "0x"}} {
		var code hexutil.Bytes
		if err := s.baseSepoliaRPC(ctx, "eth_getCode", []any{account.address, tag}, &code); err != nil {
			return err
		}
		if code.String() != account.code {
			return apperror.New(apperror.Conflict, errors.New("Base ETH account implementation changed"))
		}
	}
	return nil
}
