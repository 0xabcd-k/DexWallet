package chainassets

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math/big"
	"time"

	"filippo.io/edwards25519"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/internal/solanaspl"
	"github.com/mr-tron/base58"
	"github.com/shopspring/decimal"
)

const solanaDepositAsset = SolanaDevnet + ":sol"

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
func (r solanaDepositRoute) message(source [32]byte, blockhash []byte, lamports uint64) []byte {
	msg := []byte{2, 0, 1, 4}
	msg = append(msg, r.payer[:]...)
	msg = append(msg, source[:]...)
	msg = append(msg, r.recipient[:]...)
	msg = append(msg, make([]byte, 32)...) // System Program
	msg = append(msg, blockhash...)
	msg = append(msg, 1, 3, 2, 1, 2, 12)           // one instruction; program #3, source #1, recipient #2
	msg = binary.LittleEndian.AppendUint32(msg, 2) // SystemInstruction::Transfer
	return binary.LittleEndian.AppendUint64(msg, lamports)
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

func (s *Service) PreviewWalletDeposit(ctx context.Context, user int64, assetID, targetUSD, address string) (DepositPreview, error) {
	var empty DepositPreview
	if assetID != solanaDepositAsset {
		return empty, badTarget("wallet fee preview unsupported for asset")
	}
	route := s.quotes.solana
	if route == nil {
		return empty, apperror.New(apperror.ServiceUnavailable, fmt.Errorf("Solana wallet fee preview disabled"))
	}
	source, err := depositKey(address, true)
	if err != nil || source == route.payer || source == route.recipient {
		return empty, badTarget("invalid deposit source wallet")
	}
	start := time.Now()
	q, err := s.depositQuote(assetID, targetUSD, decimal.Zero, start)
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
	hash, _ := base58.Decode(block.Blockhash)
	entry := s.quotes.assets[assetID]
	message := func(q DepositPreview) []byte {
		// depositQuote already checked positive u64 capacity and chain precision.
		quantity, _ := decimal.NewFromString(q.PayQuantity)
		return route.message(source, hash, quantity.Shift(int32(entry.asset.Decimals)).BigInt().Uint64())
	}
	msg := message(q)
	fee, err := s.messageFee(ctx, entry.network, msg)
	if err != nil {
		return empty, apperror.New(apperror.ServiceUnavailable, err)
	}
	gas := decimal.NewFromBigInt(new(big.Int).SetUint64(fee), -int32(entry.asset.Decimals))
	q, err = s.depositQuote(assetID, targetUSD, gas, start)
	if err != nil {
		return empty, err
	}
	if fee != 0 {
		msg = message(q)
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
	q.WalletAddress, q.MessageSHA256 = address, fmt.Sprintf("%x", sha256.Sum256(msg))
	return q, nil
}
