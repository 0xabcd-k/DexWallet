import { useEffect, useState } from 'react';
import type { RewardSummaryData } from '@dexwallet/api-contract/platform';
import { getRewardSummary } from '../../api/task-rewards';

export function useRewardSummary(revision: number) {
  const [state, setState] = useState<{ data?: RewardSummaryData; error?: unknown }>({});
  useEffect(() => {
    const controller = new AbortController();
    setState({});
    void getRewardSummary(controller.signal).then(
      data => { if (!controller.signal.aborted) setState({ data }); },
      error => { if (!controller.signal.aborted) setState({ error }); },
    );
    return () => controller.abort();
  }, [revision]);
  return state;
}
