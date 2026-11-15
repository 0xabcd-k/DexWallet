// Hydrate native state before importing modules that construct the product
// session or read pending business requests at module initialization.
let ready = true;
if (import.meta.env.NATIVE_APP) {
  try { await (await import('./app/native/storage')).installNativeStorage(); }
  catch (error) {
    ready = false;
    console.error('Native storage initialization failed', error);
    const { createAppI18n } = await import('@dexwallet/i18n');
    const { appResources } = await import('./locales');
    const i18n = await createAppI18n('user', appResources);
    const root = document.getElementById('root');
    if (root) root.textContent = i18n.t('common:startupError');
  }
}
if (ready) await import('./main');
