/** Configuration paths are validated before any browser URL normalization. */
export function assertCanonicalPath(value: unknown): asserts value is string {
  if (typeof value !== 'string' || !value.startsWith('/') || value.includes('//')
    || /[\s\p{Cc}%\\?#]/u.test(value)
    || /(?:^|\/)\.{1,2}(?:\/|$)/.test(value)) {
    throw new TypeError('path must be a canonical absolute same-origin path');
  }
}
