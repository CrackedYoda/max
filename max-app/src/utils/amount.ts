/** Trims a decimal string for display without rounding up: 1.234567891 → 1.2345. */
export function formatAmount(display: string, maxDecimals = 4): string {
  const [int, frac = ''] = display.split('.');
  const trimmed = frac.slice(0, maxDecimals).replace(/0+$/, '');
  const intPart = (int ?? '0').replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  if (!trimmed && intPart === '0' && /[1-9]/.test(frac)) return `<0.${'0'.repeat(maxDecimals - 1)}1`;
  return trimmed ? `${intPart}.${trimmed}` : intPart;
}

/** Applies a keypad key to an amount string ("0", digits, one ".", bounded decimals). */
export function applyAmountKey(current: string, key: string, decimals: number): string {
  if (key === 'del') return current.length > 1 ? current.slice(0, -1) : '0';
  if (key === '.') return current.includes('.') || decimals === 0 ? current : `${current}.`;
  const [, frac] = current.split('.');
  if (frac !== undefined && frac.length >= Math.min(decimals, 9)) return current;
  if (current.replace('.', '').length >= 14) return current;
  return current === '0' ? key : current + key;
}

export const isPositiveAmount = (v: string) => /^\d*\.?\d+$/.test(v) && Number(v) > 0;
