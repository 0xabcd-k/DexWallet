import { createBackofficeRouter } from '@dexwallet/backoffice';
export const router = createBackofficeRouter(
  'merchant',
  import.meta.env.VITE_API_BASE_URL ?? '/api',
  import.meta.env.DEV
    ? [
        {
          path: '/__dev',
          lazy: async () => {
            const { DevPage } = await import('../view/dev');
            return { Component: DevPage };
          },
        },
      ]
    : [],
);
