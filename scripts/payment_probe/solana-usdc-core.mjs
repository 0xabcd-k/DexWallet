// Preserve the original Circle USDC probe interface and fixed asset selection.
import { USDC } from './solana-token-assets.mjs';
import * as token from './solana-token-core.mjs';
export const USDC_MINT = USDC.mint, USDC_DECIMALS = USDC.decimals, USDC_AMOUNT = USDC.amount;
export const MAX_ATA_RENT_LAMPORTS = token.MAX_ATA_RENT_LAMPORTS;
export const tokenAddresses = addresses => token.tokenAddresses(addresses, USDC);
export const validateUsdcSnapshot = (addresses, accounts, rent) => token.validateTokenSnapshot(addresses, accounts, rent, USDC);
export const inspectUsdcAccounts = addresses => token.inspectTokenAccounts(addresses, USDC);
export const prepareUsdcOrder = (addresses, block, snapshot, existing) => token.prepareTokenOrder(addresses, block, snapshot, existing, USDC);
export const verifyUsdcReceipt = (order, receipt) => token.verifyTokenReceipt(order, receipt, USDC);
