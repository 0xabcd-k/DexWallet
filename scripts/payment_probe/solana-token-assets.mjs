// Fixed Devnet probes only. The fixture is NOT Tether-issued USDT.
export const USDC = Object.freeze({
  symbol: 'USDC', mint: '4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU', decimals: 6, amount: 10000n,
  probePrefix: 'solana-usdc', description: 'Circle Devnet USDC',
  fundingHint: 'Test user needs Circle Devnet USDC from the official faucet',
});
export const USDT_FIXTURE = Object.freeze({
  symbol: 'USDT-FIXTURE', mint: '2ydN2Tr1FhRmJJcdqATsFCq5Q1t43bfRQD5tKfSxL7d5', decimals: 6, amount: 10000n,
  probePrefix: 'solana-usdt-fixture', description: 'self-issued Devnet SPL fixture (NOT Tether USDT)',
  fundingHint: 'Initialize the dedicated Devnet fixture first',
});
export const MAINNET_USDT_MINT = 'Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB'; // Read-only reference.
export function probeToken(symbol) {
  if (symbol === USDC.symbol) return USDC;
  if (symbol === USDT_FIXTURE.symbol) return USDT_FIXTURE;
  throw new Error('Unsupported probe asset');
}
