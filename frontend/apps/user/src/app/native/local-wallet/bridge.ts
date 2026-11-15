import { registerPlugin, type PluginListenerHandle } from '@capacitor/core';
import type { CatalogPort, VaultPort } from './repository';
export interface PrivacyState { token: string; active: boolean; revision: number }
export interface NativeWalletVault extends VaultPort {
  protect(value: { token: string }): Promise<PrivacyState>;
  acknowledge(value: { token: string; revision: number }): Promise<void>;
  unprotect(value: { token: string }): Promise<void>;
  addListener(event: 'privacyChange', listener: (state: PrivacyState) => void): Promise<PluginListenerHandle>;
}
export const nativeVault = registerPlugin<NativeWalletVault>('WalletVault');
export const nativeCatalog = registerPlugin<CatalogPort>('NativeState');
