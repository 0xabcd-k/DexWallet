import type { CapacitorConfig } from '@capacitor/cli';
// Android packages bundled assets. Remote pages are never loaded as the app UI.
const config: CapacitorConfig = {
  appId: 'com.dexwallet.app', appName: 'DexWallet', webDir: '../user/dist-app',
  server: { androidScheme: 'https' },
  android: { allowMixedContent: process.env.DEXWALLET_APP_DEBUG_HTTP === '1' },
};
export default config;
