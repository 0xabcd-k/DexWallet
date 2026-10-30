import { Button, Space } from 'antd';
import { useTranslation } from '@dexwallet/i18n';
export function Paging({
  page,
  count,
  busy,
  onPage,
}: {
  page: number;
  count: number;
  busy: boolean;
  onPage: (n: number) => void;
}) {
  const { t } = useTranslation();
  return (
    <Space className="bo-paging">
      <Button disabled={busy || page === 1} onClick={() => onPage(page - 1)}>
        {t('boPrevious')}
      </Button>
      <span>
        {t('common:page')} {page}
      </span>
      <Button disabled={busy || count < 50} onClick={() => onPage(page + 1)}>
        {t('boNext')}
      </Button>
    </Space>
  );
}
