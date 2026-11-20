package chainassets

import (
	"strings"

	"github.com/0xabcd-k/DexWallet/backend/internal/baseusdc"
)

type Family string

const (
	TON                  Family = "ton"
	Base                 Family = "base"
	Solana               Family = "solana"
	TRON                 Family = "tron"
	TONMainnet                  = "ton-mainnet"
	TONTestnet                  = "ton-testnet"
	BaseMainnet                 = "base-mainnet"
	BaseSepolia                 = "base-sepolia"
	BaseSepoliaETH              = BaseSepolia + ":eth"
	SolanaMainnet               = "solana-mainnet"
	SolanaDevnet                = "solana-devnet"
	TRONMainnet                 = "tron-mainnet"
	TRONNile                    = "tron-nile"
	Native                      = "native"
	Issuer                      = "issuer"
	TestFixture                 = "test_fixture"
	StatusOK                    = "ok"
	StatusUnavailable           = "unavailable"
	ErrorNone                   = "NONE"
	ErrorUpstream               = "UPSTREAM_UNAVAILABLE"
	ErrorInvalidResponse        = "UPSTREAM_INVALID"
	ErrorNetwork                = "NETWORK_MISMATCH"
)

type Asset struct {
	ID       string
	Symbol   string
	Contract string
	Decimals int
	Origin   string
}
type Network struct {
	ID      string
	Family  Family
	Testnet bool
	Enabled bool
	Assets  []Asset
}

func asset(network, symbol, contract string, decimals int, origin string) Asset {
	id := network + ":" + strings.ToLower(symbol)
	// Mainnet identities are immutable. A configured test substitute may change
	// on redeployment; never let an older catalog label the new token's balance.
	if origin == TestFixture {
		id += ":" + contract
	}
	return Asset{ID: id, Symbol: symbol, Contract: contract, Decimals: decimals, Origin: origin}
}

// Mainnet identities come from issuer lists, never client-supplied symbols.
// Test USDT substitutes are opt-in and can only be configured on test networks.
func catalog() []Network {
	return []Network{
		{ID: TONMainnet, Family: TON, Assets: []Asset{asset(TONMainnet, "TON", "", 9, Native), asset(TONMainnet, "USDT", "EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs", 6, Issuer)}},
		{ID: BaseMainnet, Family: Base, Assets: []Asset{asset(BaseMainnet, "ETH", "", 18, Native), asset(BaseMainnet, "USDC", "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", 6, Issuer)}},
		{ID: SolanaMainnet, Family: Solana, Assets: []Asset{asset(SolanaMainnet, "SOL", "", 9, Native), asset(SolanaMainnet, "USDC", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", 6, Issuer), asset(SolanaMainnet, "USDT", "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB", 6, Issuer)}},
		{ID: TRONMainnet, Family: TRON, Assets: []Asset{asset(TRONMainnet, "TRX", "", 6, Native), asset(TRONMainnet, "USDT", "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", 6, Issuer)}},
		{ID: TONTestnet, Family: TON, Testnet: true, Assets: []Asset{asset(TONTestnet, "TON", "", 9, Native)}},
		{ID: BaseSepolia, Family: Base, Testnet: true, Assets: []Asset{asset(BaseSepolia, "ETH", "", 18, Native), asset(BaseSepolia, "USDC", baseusdc.SepoliaContract, 6, Issuer)}},
		{ID: SolanaDevnet, Family: Solana, Testnet: true, Assets: []Asset{asset(SolanaDevnet, "SOL", "", 9, Native), asset(SolanaDevnet, "USDC", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU", 6, Issuer)}},
		{ID: TRONNile, Family: TRON, Testnet: true, Assets: []Asset{asset(TRONNile, "TRX", "", 6, Native)}},
	}
}
