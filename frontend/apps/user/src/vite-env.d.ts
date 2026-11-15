/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly NATIVE_APP: boolean;
  readonly APP_API_URL: string;
  readonly TON_MANIFEST_URL: string;
  readonly DEV_TELEGRAM: boolean;
}
