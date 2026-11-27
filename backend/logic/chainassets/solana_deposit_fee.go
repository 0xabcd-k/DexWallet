package chainassets

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"filippo.io/edwards25519"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/solanaspl"
	"github.com/mr-tron/base58"
	"github.com/shopspring/decimal"
)

const (
	devnetUSDCMint          = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
	solanaDepositAsset      = SolanaDevnet + ":sol"
	solanaDepositUSDC       = SolanaDevnet + ":usdc"
	solanaDepositUSDTPrefix = SolanaDevnet + ":usdt:"
	legacyTokenAccountSize  = 165
)

type SolanaDepositFeeConfig struct {
	FeePayer, Recipient string
}

type solanaDepositRoute struct {
	payer, recipient [32]byte
}

func depositKey(address string, signer bool) ([32]byte, error) {
	if len(address) > 44 || address == solanaspl.SystemProgram {
		return [32]byte{}, fmt.Errorf("invalid Solana deposit address")
	}
	b, err := base58.Decode(address)
	if err != nil || len(b) != 32 {
		return [32]byte{}, fmt.Errorf("invalid Solana deposit address")
	}
	if signer {
		if _, err := new(edwards25519.Point).SetBytes(b); err != nil {
			return [32]byte{}, fmt.Errorf("invalid Solana signer")
		}
	}
	return [32]byte(b), nil
}

func newSolanaDepositRoute(c SolanaDepositFeeConfig) (*solanaDepositRoute, error) {
	payer, err := depositKey(c.FeePayer, true)
	if err != nil {
		return nil, err
	}
	recipient, err := depositKey(c.Recipient, false)
	if err != nil {
		return nil, err
	}
	if payer == recipient {
		return nil, fmt.Errorf("Solana deposit fee payer and recipient must differ")
	}
	return &solanaDepositRoute{payer: payer, recipient: recipient}, nil
}

// Legacy SystemProgram.transfer. Both the fee payer and the source sign.
// The source transfers the full gross amount, including the quoted fee recovery.
func (r solanaDepositRoute) message(source [32]byte, blockhash []byte, lamports uint64, orderIdx string) []byte {
	msg := []byte{2, 0, 1, 4}
	msg = append(msg, r.payer[:]...)
	msg = append(msg, source[:]...)
	msg = append(msg, r.recipient[:]...)
	msg = append(msg, make([]byte, 32)...) // System Program
	instructions := byte(1)
	if orderIdx != "" {
		msg[2], msg[3], instructions = 2, 5, 2 // additional readonly Memo program
		memoProgram, _ := base58.Decode(solanaspl.MemoProgram)
		msg = append(msg, memoProgram...)
	}
	msg = append(msg, blockhash...)
	msg = append(msg, instructions, 3, 2, 1, 2, 12) // one instruction; program #3, source #1, recipient #2
	msg = binary.LittleEndian.AppendUint32(msg, 2)  // SystemInstruction::Transfer
	msg = binary.LittleEndian.AppendUint64(msg, lamports)
	if orderIdx != "" {
		memo := "dexwallet:deposit:" + orderIdx
		msg = append(msg, 4, 0, byte(len(memo)))
		msg = append(msg, memo...)
	}
	return msg
}

func (s *Service) messageFee(ctx context.Context, n Network, msg []byte) (uint64, error) {
	var response struct {
		Context struct{ Slot uint64 }
		Value   *uint64
	}
	err := s.rpc(ctx, n, s.endpoints[n.ID], "getFeeForMessage", []any{
		base64.StdEncoding.EncodeToString(msg), map[string]string{"commitment": "confirmed"},
	}, &response)
	if err != nil {
		return 0, err
	}
	if response.Context.Slot == 0 || response.Value == nil {
		return 0, invalid("missing Solana fee or expired blockhash")
	}
	return *response.Value, nil
}

func (s *Service) previewSolanaDeposit(ctx context.Context, user int64, assetID, targetUSD, address string) (DepositPreview, error) {
	var empty DepositPreview
	route := s.quotes.solana[assetID]
	if route == nil {
		mint := s.endpoints[SolanaDevnet].TestUSDTContract
		if assetID != solanaDepositAsset && assetID != solanaDepositUSDC && (mint == "" || assetID != solanaDepositUSDTPrefix+mint) {
			return empty, badTarget("wallet fee preview unsupported for asset")
		}
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Solana wallet fee preview disabled"))
	}
	source, err := depositKey(address, true)
	if err != nil || source == route.payer || source == route.recipient {
		return empty, badTarget("invalid deposit source wallet")
	}
	start := time.Now()
	prices, err := s.depositPrices(ctx, assetID, start)
	if err != nil {
		return empty, err
	}
	q, err := s.depositQuote(assetID, targetUSD, decimal.Zero, start, prices)
	if err != nil {
		return empty, err
	}
	if s.limiter == nil {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("chain query limiter not configured"))
	}
	if err := s.limiter.Allow(ctx, user); err != nil {
		return empty, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	block, err := s.LatestSolanaBlock(ctx) // verifies the Devnet genesis and blockhash
	if err != nil {
		return empty, apperror.New(apperror.ServiceUnavailable, err)
	}
	entry := s.quotes.assets[assetID]
	draft := solanaDepositDraft{route: route, source: source, blockhash: block.Blockhash, decimals: entry.asset.Decimals}
	if s.quotes.solReviewAfter[assetID] > 0 {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return empty, err
		}
		draft.orderIdx = hex.EncodeToString(nonce[:])
	}
	if entry.asset.Contract != "" {
		draft.token, err = s.depositTokenAccounts(ctx, entry, address, base58.Encode(route.recipient[:]), draft.atoms(q))
		if err != nil {
			return empty, err
		}
	}
	msg, err := draft.message(q)
	if err != nil {
		return empty, err
	}
	fee, err := s.messageFee(ctx, entry.network, msg)
	if err != nil {
		return empty, apperror.New(apperror.ServiceUnavailable, err)
	}
	solScale := -int32(entry.native.Decimals)
	gas := decimal.NewFromBigInt(new(big.Int).SetUint64(fee), solScale)
	q, err = s.depositQuote(assetID, targetUSD, gas, start, prices)
	if err != nil {
		return empty, err
	}
	if fee != 0 {
		msg, err = draft.message(q)
		if err != nil {
			return empty, err
		}
		finalFee, err := s.messageFee(ctx, entry.network, msg)
		if err != nil {
			return empty, apperror.New(apperror.ServiceUnavailable, err)
		}
		if finalFee != fee {
			return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Solana fee changed for final deposit message"))
		}
	}
	if time.Now().UnixMilli() >= q.ExpiresAt {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Solana deposit quote expired during estimation"))
	}
	if draft.token != nil {
		funding := decimal.NewFromBigInt(new(big.Int).SetUint64(draft.token.rent), solScale)
		q.AccountFunding, q.AccountFundingUSD = funding.String(), funding.Mul(prices.native).String()
	}
	if draft.orderIdx != "" {
		q.SolOrderIdx, q.SolMessage = draft.orderIdx, base64.StdEncoding.EncodeToString(msg)
		q.SolLastValidHeight, q.SolReviewAt = block.LastValidBlockHeight, time.UnixMilli(q.ExpiresAt).Add(s.quotes.solReviewAfter[assetID]).UnixMilli()
	}
	q.Recipient, q.FeePayer = base58.Encode(route.recipient[:]), base58.Encode(route.payer[:])
	q.WalletAddress, q.MessageSHA256 = address, fmt.Sprintf("%x", sha256.Sum256(msg))
	return q, nil
}

type solanaDepositDraft struct {
	orderIdx  string
	route     *solanaDepositRoute
	source    [32]byte
	blockhash string
	decimals  int
	token     *depositTokenAccounts
}

func (d solanaDepositDraft) atoms(q DepositPreview) uint64 {
	// depositQuote checked the gross amount against the asset's u64 precision.
	quantity, _ := decimal.NewFromString(q.PayQuantity)
	return quantity.Shift(int32(d.decimals)).BigInt().Uint64()
}

func (d solanaDepositDraft) message(q DepositPreview) ([]byte, error) {
	amount := d.atoms(q)
	if d.token == nil {
		hash, _ := base58.Decode(d.blockhash) // LatestSolanaBlock validated the blockhash.
		return d.route.message(d.source, hash, amount, d.orderIdx), nil
	}
	if amount > d.token.balance {
		return nil, apperror.New(apperror.InsufficientBalance, nil)
	}
	transfer := d.token.transfer
	transfer.Amount = amount
	memo := ""
	if d.orderIdx != "" {
		memo = "dexwallet:deposit:" + d.orderIdx
	}
	msg, err := transfer.SponsoredMemoMessage(d.blockhash, base58.Encode(d.route.payer[:]), memo)
	if err != nil {
		return nil, apperror.New(apperror.ServiceUnavailable, err)
	}
	return msg, nil
}

type depositTokenAccounts struct {
	transfer      solanaspl.Transfer
	balance, rent uint64
}

func (s *Service) depositTokenAccounts(ctx context.Context, entry depositAsset, source, recipient string, amount uint64) (*depositTokenAccounts, error) {
	t, err := solanaspl.New(source, recipient, entry.asset.Contract, strconv.FormatUint(amount, 10), false)
	if err != nil {
		return nil, badTarget(err.Error())
	}
	var snapshot struct {
		Context struct{ Slot uint64 }
		Value   []json.RawMessage
	}
	endpoint := s.endpoints[entry.network.ID]
	err = s.rpc(ctx, entry.network, endpoint, "getMultipleAccounts", []any{[]string{t.SourceATA, t.DestinationATA}, map[string]string{"encoding": "jsonParsed", "commitment": "confirmed"}}, &snapshot)
	if err != nil {
		return nil, apperror.New(apperror.ServiceUnavailable, err)
	}
	if snapshot.Context.Slot == 0 || len(snapshot.Value) != 2 {
		return nil, apperror.New(apperror.ServiceUnavailable, invalid("incomplete SPL account observation"))
	}
	if string(snapshot.Value[0]) == "null" {
		return nil, apperror.New(apperror.InsufficientBalance, nil)
	}
	balance, err := solanaTokenBalance(snapshot.Value[0], source, t.Mint)
	if err != nil {
		return nil, apperror.New(apperror.ServiceUnavailable, err)
	}
	t.CreateATA = string(snapshot.Value[1]) == "null"
	result := &depositTokenAccounts{transfer: t, balance: balance}
	if !t.CreateATA {
		_, err = solanaTokenBalance(snapshot.Value[1], recipient, t.Mint)
	} else {
		err = s.rpc(ctx, entry.network, endpoint, "getMinimumBalanceForRentExemption", []any{legacyTokenAccountSize, map[string]string{"commitment": "confirmed"}}, &result.rent)
		if err == nil && result.rent == 0 {
			err = invalid("missing SPL account storage funding")
		}
	}
	if err != nil {
		return nil, apperror.New(apperror.ServiceUnavailable, err)
	}
	return result, nil
}
