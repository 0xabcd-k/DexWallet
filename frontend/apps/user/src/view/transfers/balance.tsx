import { Button, Dialog } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import Balances from '../balances';
import Records from '../records';
import { useAuth } from '../../auth/context';
export default function BalanceDetail() {
  const { t } = useTranslation(); const { launch } = useAuth();
  return <section className="transfer-page"><h1>{t('usdBalance')}</h1><Button block onClick={() => { void Dialog.alert({ title: t('deposit'), content: t(launch.mode === 'telegram' ? 'chainRequiresApp' : 'chainPending'), confirmText: t('confirm') }); }}>{t('deposit')}</Button><Balances section="wallet" /><h2>{t('accountEntries')}</h2><Records /></section>;
}
