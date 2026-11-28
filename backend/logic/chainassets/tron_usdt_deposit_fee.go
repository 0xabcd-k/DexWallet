package chainassets

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/tronnative"
)

const (
	NileDepositUSDTContract = "TYGk3YPGFa7ZLhXfDBd5QzN9Ms9DmPC9tn"
	nileUSDTRuntimeSHA256   = "7ae2015d27ec7aec7c057f4a3dd026dfc224a0b9ee4017eccfa569ae167cbb2b"
	nileUSDTFeeLimitSun     = 15_000_000 // Cap used by the verified single-signature test payment.
	trc20TransferTopic      = "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
)

var nileDepositUSDTID = NileUSDT(NileDepositUSDTContract).ID

func nileEstimateUnavailable(reason string) error {
	return apperror.New(apperror.ServiceUnavailable, invalid(reason))
}

type nileTriggerValue struct {
	Owner      string `json:"owner_address"`
	Contract   string `json:"contract_address"`
	Data       string
	CallValue  int64 `json:"call_value"`
	TokenValue int64 `json:"call_token_value"`
	TokenID    int64 `json:"token_id"`
}

type nileConstantResult struct {
	Result struct{ Result bool }
	Words  []string `json:"constant_result"`
	Energy int64    `json:"energy_used"`
	Logs   []struct {
		Address, Data string
		Topics        []string
	}
	Transaction struct {
		Ret []struct{ Ret, ContractRet string }
		Raw struct {
			Contract []struct {
				Type      string
				Parameter struct{ Value nileTriggerValue }
			}
		} `json:"raw_data"`
	}
}

func (v nileTriggerValue) simulate(read nileQuoteRead) (nileConstantResult, *big.Int, error) {
	var r nileConstantResult
	err := read("triggerconstantcontract", map[string]any{"owner_address": v.Owner, "contract_address": v.Contract, "data": v.Data}, &r)
	if err != nil {
		return r, nil, err
	}
	c := r.Transaction.Raw.Contract
	if !r.Result.Result || len(r.Words) != 1 || len(c) != 1 || c[0].Type != "TriggerSmartContract" || c[0].Parameter.Value != v {
		return r, nil, nileEstimateUnavailable("Nile simulation does not match the requested call")
	}
	for _, status := range r.Transaction.Ret {
		if (status.Ret != "" && status.Ret != "SUCESS") || (status.ContractRet != "" && status.ContractRet != "SUCCESS") {
			return r, nil, nileEstimateUnavailable("Nile contract simulation failed")
		}
	}
	word, err := hexInteger(r.Words[0], true)
	if err != nil {
		return r, nil, apperror.New(apperror.ServiceUnavailable, err)
	}
	return r, word, nil
}

func checkNileTestUSDT(read nileQuoteRead, token string) error {
	var info struct {
		Runtime  string `json:"runtimecode"`
		Contract struct {
			Address       string `json:"contract_address"`
			CallerPercent int    `json:"consume_user_resource_percent"`
		} `json:"smart_contract"`
	}
	if err := read("getcontractinfo", map[string]any{"value": token}, &info); err != nil {
		return err
	}
	code, err := hex.DecodeString(info.Runtime)
	if err != nil || !strings.EqualFold(info.Contract.Address, token) || fmt.Sprintf("%x", sha256.Sum256(code)) != nileUSDTRuntimeSHA256 || info.Contract.CallerPercent != 100 {
		return nileEstimateUnavailable("unexpected Nile USDT contract code or Energy sharing")
	}
	return nil
}

func checkNileEnergyCap(read nileQuoteRead, energy, capSun int64) error {
	var params struct {
		ChainParameter []struct {
			Key   string
			Value int64
		}
	}
	if err := read("getchainparameters", map[string]any{}, &params); err != nil {
		return err
	}
	var price, maxCap int64
	for _, p := range params.ChainParameter {
		switch p.Key {
		case "getEnergyFee":
			price = p.Value
		case "getMaxFeeLimit":
			maxCap = p.Value
		}
	}
	if price <= 0 || capSun > maxCap || energy > capSun/price {
		return nileEstimateUnavailable("Nile fee limit cannot cover simulated Energy at current chain parameters")
	}
	return nil
}

func estimateNileTokenEnergy(read nileQuoteRead, wallet, recipient string, tx tronnative.Transfer) (int64, error) {
	token, _ := tronnative.AddressBytes(tx.TokenContract) // Bound to the supported asset at service construction.
	tokenHex := hex.EncodeToString(token)
	if err := checkNileTestUSDT(read, tokenHex); err != nil {
		return 0, err
	}
	ownerWord := strings.Repeat("0", 24) + wallet[2:]
	call := nileTriggerValue{Owner: wallet, Contract: tokenHex, Data: balanceOfSelector + ownerWord}
	_, balance, err := call.simulate(read)
	if err != nil {
		return 0, err
	}
	atoms, _ := new(big.Int).SetString(tx.TokenAtoms, 10)
	if balance.Cmp(atoms) < 0 {
		return 0, apperror.New(apperror.InsufficientBalance, nil)
	}
	data, err := tronnative.TransferData(tx.Recipient, tx.TokenAtoms)
	if err != nil {
		return 0, err
	}
	call.Data = hex.EncodeToString(data)
	result, word, err := call.simulate(read)
	if err != nil {
		return 0, err
	}
	// This specific Nile runtime returns a zero boolean on a successful transfer.
	// Its emitted Transfer must prove the token, source, payee and full amount.
	if word.BitLen() > 1 || result.Energy <= 0 || len(result.Logs) != 1 {
		return 0, nileEstimateUnavailable("Nile USDT transfer simulation missing Energy or Transfer")
	}
	log := result.Logs[0]
	if !strings.EqualFold(log.Address, tokenHex[2:]) || len(log.Topics) != 3 || !strings.EqualFold(log.Topics[0], trc20TransferTopic) || !strings.EqualFold(log.Topics[1], ownerWord) || !strings.EqualFold(log.Topics[2], strings.Repeat("0", 24)+recipient[2:]) || !strings.EqualFold(log.Data, fmt.Sprintf("%064x", atoms)) {
		return 0, nileEstimateUnavailable("unexpected Nile USDT Transfer event")
	}
	if err := checkNileEnergyCap(read, result.Energy, tx.FeeLimitSun); err != nil {
		return 0, err
	}
	return result.Energy, nil
}
