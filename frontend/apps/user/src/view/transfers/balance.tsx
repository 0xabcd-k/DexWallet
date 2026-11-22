import { lazy, Suspense } from 'react';
import { Button, Dialog } from 'antd-mobile';
import { useTranslation } from '@dexwallet/i18n';
import Balances from '../balances';
import Records from '../records';
import { useAuth } from '../../auth/context';

const NativeDeposit = import.meta.env.NATIVE_APP ? lazy(() => import('../chain-transfer/deposit')) : undefined;

export default function BalanceDetail() {
  const { t } = useTranslation(), { launch, session } = useAuth();
  const userIdx = session.credential!.idx;
  return <section className="transfer-page">
    <h1>{t('usdBalance')}</h1>
    {NativeDeposit
      ? <Suspense fallback={<p role="status">{t('common:loading')}</p>}><NativeDeposit key={userIdx} userIdx={userIdx}/></Suspense>
      : <Button block onClick={() => { void Dialog.alert({ title: t('deposit'), content: t(launch.mode === 'telegram' ? 'chainRequiresApp' : 'chainPending'), confirmText: t('confirm') }); }}>{t('deposit')}</Button>}
    <Balances section="wallet"/><h2>{t('accountEntries')}</h2><Records/>
  </section>;
}
