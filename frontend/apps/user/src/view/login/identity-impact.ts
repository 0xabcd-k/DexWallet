import { Dialog } from 'antd-mobile';
import { ApiError, decodeData } from '@dexwallet/api-client';
import type { IdentityApproval, IdentityImpactData, IdentityImpactRequest } from '@dexwallet/api-contract/platform';
import type { useTranslation } from '@dexwallet/i18n';
type Translate = ReturnType<typeof useTranslation>['t'];
import { userRequest } from '../../api/account';
import { session, type Session } from '../../auth/session';
export async function approveIdentity(input: IdentityImpactRequest, ticket: Session, t: Translate, allowReplacement = true): Promise<IdentityApproval | undefined> {
  const impact = await userRequest('/v1/user/identities/impact', decodeData<IdentityImpactData>, { method: 'POST', body: input }, ticket);
  if (impact.replacement && !allowReplacement) throw new ApiError('business', 403, 'PERMISSION_DENIED');
  if (impact.replacement && !await Dialog.confirm({ title: t('replacementTitle'), content: t('replacementWarning'), confirmText: t('confirm'), cancelText: t('cancel') })) return;
  if (!session.matches(ticket)) throw new ApiError('cancelled');
  return { expectedRevision: impact.revision, expectedReplacement: impact.replacement };
}
