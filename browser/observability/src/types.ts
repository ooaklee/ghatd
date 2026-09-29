/** Generic outcome and facade contracts shared by every integration module. */
export type RequestOutcome = 'success' | 'http-error' | 'network-error' | 'cancelled' | 'timeout';
export type NavigationOutcome = 'complete' | 'cancelled' | 'redirected' | 'error' | 'timeout';
export type ErrorSource = 'vue' | 'window' | 'unhandledrejection' | 'navigation';

export interface RequestMeasurement {
  readonly traceparent: string;
  valid(): boolean;
  finish(outcome: RequestOutcome, status?: number): void;
}

export interface BrowserTelemetry {
  beginNavigation(key: object, routeName: unknown, redirected: boolean): void;
  completeNavigation(key: object, outcome: NavigationOutcome): void;
  recordError(error: unknown, source: ErrorSource): void;
  startRequest(method: string, group: string): RequestMeasurement | undefined;
  refreshConsent(): void;
  forceFlush(): Promise<void>;
  stop(): Promise<void>;
}

/** Generic consent surface supplied by the host application. */
export interface ConsentSource {
  /** Whether analytics telemetry is currently allowed. Must be side-effect free. */
  isAllowed(): boolean;
}

/** Consent surface extended by optional expiry-based fail-closed behavior. */
export interface ExpiringConsentSource extends ConsentSource {
  /**
   * Epoch milliseconds when the current consent grant expires, or undefined
   * when unknown. Undefined, invalid, or expired values deny consent. The
   * controller fails closed at that instant without ever
   * canceling an in-flight business request.
   */
  getExpiresAt(): number | undefined;
}

/** Generic change-notification surface supplied by the host application. */
export interface ConsentChangeBus {
  /** Subscribes to consent changes; returns an unsubscribe function. */
  subscribe(listener: () => void): () => void;
}

/** Optional framework hooks; the controller has no framework runtime dependency. */
export interface BrowserFrameworkIntegration {
  /** Installs an error listener and returns a cleanup that removes only its own listener. */
  subscribeErrors(record: (error: unknown, source: ErrorSource) => void): () => void;
  /** Resolves after the framework has applied pending view updates. */
  postRender(): Promise<void>;
}
