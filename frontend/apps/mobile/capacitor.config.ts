import type { CapacitorConfig } from '@capacitor/cli';
// Android packages bundled assets. Remote pages are never loaded as the app UI.
const config: CapacitorConfig = {
  appId: 'com.dexwallet.app', appName: 'DexWallet', webDir: '../user/dist-app',
  // Capacitor's debug logger includes native-call arguments, including secrets.
  loggingBehavior: 'none',
  server: { androidScheme: 'https' },
  android: { allowMixedContent: process.env.DEXWALLET_APP_DEBUG_HTTP === '1' },
};
export default config;
