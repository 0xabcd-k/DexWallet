import { useTranslation } from '@dexwallet/i18n';
import { Unavailable } from '@dexwallet/ui-desktop';
export default function Overview() { const { t } = useTranslation(); return <Unavailable title={t('overview')} />; }
