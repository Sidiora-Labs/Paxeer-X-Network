const NUMERIC_REGEX = /^[0-9]*\.?[0-9]*$/;

export function isValidNumericInput(value: string, maxDecimals: number = 18): boolean {
  if (value === '' || value === '.') return true;
  if (!NUMERIC_REGEX.test(value)) return false;

  // Check decimal precision
  const dotIndex = value.indexOf('.');
  if (dotIndex !== -1) {
    const decimals = value.length - dotIndex - 1;
    if (decimals > maxDecimals) return false;
  }

  return true;
}

export function sanitizeNumericInput(value: string, maxDecimals: number = 18): string {
  // Strip non-numeric chars except dot
  let sanitized = value.replace(/[^0-9.]/g, '');

  // Only allow one dot
  const firstDot = sanitized.indexOf('.');
  if (firstDot !== -1) {
    sanitized = sanitized.slice(0, firstDot + 1) + sanitized.slice(firstDot + 1).replace(/\./g, '');
  }

  // Trim excess decimals
  const dotIdx = sanitized.indexOf('.');
  if (dotIdx !== -1 && sanitized.length - dotIdx - 1 > maxDecimals) {
    sanitized = sanitized.slice(0, dotIdx + 1 + maxDecimals);
  }

  return sanitized;
}
