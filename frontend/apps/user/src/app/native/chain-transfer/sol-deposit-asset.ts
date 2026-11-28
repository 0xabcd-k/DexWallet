import { base58 } from '@scure/base';

export const solanaDepositSOL = 'solana-devnet:sol';
export const solanaDepositUSDC = 'solana-devnet:usdc';
export const solanaTestUSDTPrefix = 'solana-devnet:usdt:';
const usdcMint = '4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU';

export function depositTokenMint(assetId: string): Uint8Array | undefined {
  if (assetId === solanaDepositUSDC) return base58.decode(usdcMint);
  if (typeof assetId !== 'string' || !assetId.startsWith(solanaTestUSDTPrefix)) return;
  const address = assetId.slice(solanaTestUSDTPrefix.length);
  if (!/^[1-9A-HJ-NP-Za-km-z]{32,44}$/.test(address) || address === usdcMint) return;
  const mint = base58.decode(address);
  if (mint.length === 32 && mint.some(byte => byte !== 0)) return mint;
}
