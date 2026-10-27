import type { HealthResponse, ErrorResponse } from '@dexwallet/api-contract/platform';
const live: HealthResponse = { code: 'OK', message: '', data: { status: 'live' }, traceId: 'trace' };
const failed: ErrorResponse = { code: 'SERVICE_UNAVAILABLE', message: 'Unavailable', traceId: 'trace' };
// @ts-expect-error Failure does not contain data.
failed.data;
// @ts-expect-error Unknown code is not in the published contract.
const bad: ErrorResponse = { code: 'SUCCESS', message: '', traceId: '' };
// @ts-expect-error Health data cannot be null.
const empty: HealthResponse = { ...live, data: null };
void [live, failed, bad, empty];
