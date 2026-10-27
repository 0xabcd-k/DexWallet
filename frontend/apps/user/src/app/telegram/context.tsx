import { createContext, useContext } from 'react';
import type { LaunchContext } from './bootstrap';
export const LaunchProvider = createContext<Readonly<LaunchContext> | undefined>(undefined);
export function useLaunchContext() {
  const value = useContext(LaunchProvider);
  if (!value) throw new Error('LaunchProvider is required');
  return value;
}
