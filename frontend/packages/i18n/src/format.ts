import type { Language } from './index';
// Display only: no arithmetic or rounding policy is introduced here.
export function formatAmount(value: string | null, unit: string): string | null {
  if (value === null) return null;
  if (!/^-?\d+(?:\.\d+)?$/.test(value) || !unit.trim()) throw new TypeError('Invalid decimal amount or unit');
  const [whole = '', fraction] = value.split('.');
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  return `${grouped}${fraction === undefined ? '' : `.${fraction}`} ${unit}`;
}
export function formatTime(epochMilliseconds: number, language: Language, timeZone?: string): string {
  if (!Number.isFinite(epochMilliseconds) || Number.isNaN(new Date(epochMilliseconds).getTime())) throw new TypeError('Invalid timestamp');
  return new Intl.DateTimeFormat(language, {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit',
    timeZoneName: 'shortOffset', ...(timeZone ? { timeZone } : {}),
  }).format(epochMilliseconds);
}
