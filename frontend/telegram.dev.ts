import { createHmac, randomUUID } from 'node:crypto';
import type { Plugin } from 'vite';

// Runs in Vite's Node process. The bot token never enters browser configuration.
export function telegramDev(): Plugin {
  const botToken = process.env.DEXWALLET_TELEGRAM_BOT_TOKEN;
  if (!botToken) throw new Error('Telegram development requires DEXWALLET_TELEGRAM_BOT_TOKEN matching the local API.');
  return {
    name: 'dexwallet-telegram-development', apply: 'serve',
    configureServer(server) {
      server.middlewares.use('/__dev/telegram-launch', (req, res, next) => {
        if (req.method !== 'POST') return next();
        const init = new URLSearchParams({
          auth_date: String(Math.floor(Date.now() / 1000)), query_id: randomUUID(),
          // SDK schema requires this field; this development flow uses bot HMAC verification.
          signature: '',
          user: JSON.stringify({ id: 900000001, first_name: '开发体验', last_name: '用户', username: 'dexwallet_local_dev', language_code: 'zh-hans' }),
        });
        init.sort();
        const secret = createHmac('sha256', 'WebAppData').update(botToken).digest();
        init.set('hash', createHmac('sha256', secret).update(Array.from(init, ([key, value]) => `${key}=${value}`).join('\n')).digest('hex'));
        const launch = new URLSearchParams({ tgWebAppData: init.toString(), tgWebAppVersion: '8.0', tgWebAppPlatform: 'tdesktop', tgWebAppThemeParams: '{}' });
        res.setHeader('Content-Type', 'application/json');
        res.setHeader('Cache-Control', 'no-store');
        res.end(JSON.stringify({ launchParams: launch.toString() }));
      });
    },
  };
}
