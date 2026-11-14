import { decodeData, isRecord, type Decoder } from '@dexwallet/api-client';
import type { TaskData, TaskListData, TaskCheckData } from '@dexwallet/api-contract/platform';
import { userRequest } from './account';
export const TASK_PAGE_SIZE = 20;
export const getTasks = (before: string, signal: AbortSignal) => userRequest(`/v1/user/tasks?${new URLSearchParams({ before, limit: String(TASK_PAGE_SIZE) })}`, decodeData<TaskListData>, { signal });
export const getTask = (idx: string, signal: AbortSignal) => userRequest(`/v1/user/tasks/${encodeURIComponent(idx)}`, decodeData<TaskData>, { signal });
const decodeCheck: Decoder<TaskCheckData> = value => {
  if (!isRecord(value) || !isRecord(value.data) || !isRecord(value.data.task) ||
    !['member', 'not_member'].includes(String(value.data.outcome)) || typeof value.data.checkedAt !== 'string' ||
    !Number.isFinite(Date.parse(value.data.checkedAt))) throw new TypeError('Invalid task check');
  return decodeData<TaskCheckData>(value);
};
export const checkTask = (idx: string, signal: AbortSignal) => userRequest(`/v1/user/tasks/${encodeURIComponent(idx)}/check`, decodeCheck, { method: 'POST', signal });
