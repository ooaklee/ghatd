/** Synthetic consent source shared by tests: no host cookie/storage fields. */
import type { ConsentChangeBus, ExpiringConsentSource } from '../src/types';

export interface SyntheticConsent extends ExpiringConsentSource {
  grant(expiresAtMs?: number): void;
  revoke(): void;
  readonly changes: ConsentChangeBus;
}

export function syntheticConsent(initiallyAllowed = false): SyntheticConsent {
  let allowed = initiallyAllowed;
  let expiresAt: number | undefined = initiallyAllowed ? Date.now() + 86400000 : undefined;
  const listeners = new Set<() => void>();
  return {
    isAllowed: () => allowed,
    getExpiresAt: () => expiresAt,
    grant(expiresAtMs) {
      allowed = true;
      expiresAt = expiresAtMs ?? Date.now() + 86400000;
      for (const listener of listeners) listener();
    },
    revoke() {
      allowed = false;
      expiresAt = undefined;
      for (const listener of listeners) listener();
    },
    changes: {
      subscribe(listener) {
        listeners.add(listener);
        return () => listeners.delete(listener);
      },
    },
  };
}
