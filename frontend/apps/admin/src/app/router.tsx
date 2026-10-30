import { createBackofficeRouter } from '@dexwallet/backoffice';
export const router = createBackofficeRouter(
  'admin',
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
