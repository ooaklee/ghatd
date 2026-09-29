/**
 * Consent-scoped browser telemetry controller.
 *
 * The controller and exporter own the OpenTelemetry SDK imports; hosts that use
 * the facade, group vocabulary or fetch/router/axios integrations keep the SDK
 * out of their bundle until this module is imported lazily.
 */
import { ROOT_CONTEXT, SpanKind, SpanStatusCode, trace, type Attributes, type Span } from '@opentelemetry/api';
import { resourceFromAttributes } from '@opentelemetry/resources';
import { BatchSpanProcessor, TracerProvider, type BatchSpanProcessorBrowserOptions } from '@opentelemetry/sdk-trace';
import type { BrowserFrameworkIntegration, BrowserTelemetry, ConsentChangeBus, ConsentSource, ErrorSource, ExpiringConsentSource, NavigationOutcome, RequestOutcome } from './types';
import { consentExporter } from './exporter';
import { assertCanonicalPath } from './paths';
import { createGroups, errorType, requestMethod, type GroupConfig, type RouteGroups } from './groups';

const MAX_ACTIVE = 128;
const MAX_DURATION = 120000;
const MAX_TIMER = 2147483647;

type Measurement = { span: Span; done: boolean; timer: ReturnType<typeof setTimeout>; finish(outcome?: string, status?: number): void };
type Generation = {
  provider: TracerProvider;
  exporter: ReturnType<typeof consentExporter>;
  active: Set<Measurement>;
  closed: boolean;
};

export interface BrowserTelemetryOptions {
  /** Required generic consent surface supplied by the host. */
  consent: ConsentSource | ExpiringConsentSource;
  /** Optional generic change notification; defaults to a no-op subscription. */
  changes?: ConsentChangeBus;
  /** Host identity for resource attributes; generic, e.g. "my-web-app". */
  serviceName: string;
  /** Host scope name for tracer identity, e.g. "github.com/your-org/your-app". */
  scope: string;
  /** Static finite route/API group configuration. */
  groups: GroupConfig;
  /** Absolute same-origin intake path for OTLP/JSON trace export. */
  intakePath: string;
  /** Optional framework error subscription and post-render tick. */
  framework?: BrowserFrameworkIntegration;
  /** Transport override, primarily for tests. */
  fetch?: typeof fetch;
}

/** One private provider per consent generation; no global OTel installation. */
export function createBrowserTelemetry(options: BrowserTelemetryOptions): BrowserTelemetry {
  const consentSource: ConsentSource & Partial<ExpiringConsentSource> = options.consent;
  const send = options.fetch ?? globalThis.fetch.bind(globalThis);
  const intakePath = options.intakePath;
  assertCanonicalPath(intakePath);
  const groups: RouteGroups = createGroups({ ...options.groups, intakePath });
  let disposed = false;
  let generation: Generation | undefined;
  let group = 'other';
  let navigation: { key: object; measurement: Measurement } | undefined;
  let expiryTimer: ReturnType<typeof setTimeout> | undefined;
  let documentPending = false;
  const closing = new Set<Promise<void>>();
  const frames = new Set<number>();
  function cancelFrames(): void { for (const frame of frames) cancelAnimationFrame(frame); frames.clear(); }

  function readConsent(): { expiresAt?: number } | undefined {
    try {
      if (disposed || consentSource.isAllowed() !== true) return;
      if (!('getExpiresAt' in consentSource)) return {};
      if (typeof consentSource.getExpiresAt !== 'function') return;
      const expiresAt = consentSource.getExpiresAt();
      if (typeof expiresAt !== 'number' || !Number.isFinite(expiresAt) || expiresAt <= Date.now()) return;
      return { expiresAt };
    } catch { return; }
  }
  function allowed(): boolean { return readConsent() !== undefined; }
  function closeGeneration(): void {
    const previous = generation;
    generation = undefined;
    navigation = undefined;
    documentPending = false;
    clearTimeout(expiryTimer);
    cancelFrames();
    if (!previous) return;
    previous.closed = true;
    // Close the export gate BEFORE shutdown, which normally flushes queued spans.
    previous.exporter.close();
    for (const measurement of previous.active) { clearTimeout(measurement.timer); measurement.done = true; }
    previous.active.clear();
    const shutdown = previous.provider.shutdown().catch(() => {}).finally(() => closing.delete(shutdown));
    closing.add(shutdown);
  }
  function refreshConsent(): void {
    const consent = readConsent();
    if (!consent) { closeGeneration(); return; }
    clearTimeout(expiryTimer);
    // Expiry is independently enforced even if the host's boolean stays true.
    if (consent.expiresAt !== undefined) {
      expiryTimer = setTimeout(refreshConsent, Math.min(MAX_TIMER, Math.max(1, consent.expiresAt - Date.now())));
    }
    if (generation) return;
    const next = { active: new Set<Measurement>(), closed: false } as Generation;
    next.exporter = consentExporter(() => generation === next && !next.closed && allowed(), send, intakePath);
    const processorOptions: BatchSpanProcessorBrowserOptions = {
      exporter: next.exporter, maxQueueSize: 128, maxExportBatchSize: 16,
      // Eight serial export batches can each consume the 3s transport deadline.
      scheduledDelayMillis: 5000, exportTimeoutMillis: 25000,
      disableAutoFlushOnDocumentHide: true,
    };
    next.provider = new TracerProvider({
      resource: resourceFromAttributes({ 'service.name': options.serviceName }),
      spanLimits: { attributeCountLimit: 16, attributeValueLengthLimit: 32, eventCountLimit: 0, linkCountLimit: 0 },
      spanProcessors: [new BatchSpanProcessor(processorOptions)],
    });
    generation = next;
  }
  function start(name: string, attributes: Attributes, kind = SpanKind.INTERNAL, parent = true): Measurement | undefined {
    refreshConsent();
    const owner = generation;
    if (!owner || owner.active.size >= MAX_ACTIVE) return;
    const context = parent && navigation && !navigation.measurement.done
      ? trace.setSpan(ROOT_CONTEXT, navigation.measurement.span) : ROOT_CONTEXT;
    const started = Date.now();
    const span = owner.provider.getTracer(options.scope, '1').startSpan(name, { kind, attributes, startTime: started }, context);
    const measurement: Measurement = {
      span, done: false,
      timer: setTimeout(() => measurement.finish('timeout'), MAX_DURATION),
      finish(outcome, status) {
        if (measurement.done) return;
        measurement.done = true;
        clearTimeout(measurement.timer);
        owner.active.delete(measurement);
        if (owner.closed || generation !== owner || !allowed()) { refreshConsent(); return; }
        if (outcome) span.setAttribute('browser.outcome', outcome);
        if (Number.isInteger(status) && status! >= 100 && status! <= 599) span.setAttribute('http.response.status_code', status!);
        if (name === 'browser.error' || ['error', 'http-error', 'network-error', 'timeout'].includes(outcome ?? '')) {
          span.setStatus({ code: SpanStatusCode.ERROR });
        }
        // Background tabs can delay the timeout callback beyond the intake bound.
        span.end(Math.max(started, Math.min(Date.now(), started + MAX_DURATION)));
      },
    };
    owner.active.add(measurement);
    return measurement;
  }
  function recordError(error: unknown, source: ErrorSource): void {
    if (!['vue', 'window', 'unhandledrejection', 'navigation'].includes(source)) return;
    start('browser.error', {
      'browser.route.group': group, 'browser.error.source': source, 'error.type': errorType(error),
    })?.finish();
  }
  const windowError = (event: ErrorEvent) => recordError(event.error, 'window');
  const rejection = (event: PromiseRejectionEvent) => recordError(event.reason, 'unhandledrejection');
  const consentChanged = () => refreshConsent();
  const unsubscribe = options.changes?.subscribe(consentChanged);
  const postRender = options.framework ? () => options.framework!.postRender() : () => Promise.resolve();
  const cleanupFramework = options.framework?.subscribeErrors(recordError);
  window.addEventListener('error', windowError);
  window.addEventListener('unhandledrejection', rejection);

  // Navigation Timing is eligible only for consent already present at setup.
  const initialConsent = allowed();
  documentPending = initialConsent;
  function documentTiming(): void {
    if (!documentPending) return;
    documentPending = false;
    if (!allowed()) return;
    const timing = performance.getEntriesByType?.('navigation')[0] as PerformanceNavigationTiming | undefined;
    if (!timing) return;
    const attributes: Attributes = { 'browser.route.group': group };
    for (const [key, value] of [
      ['ttfb_ms', timing.responseStart - timing.requestStart],
      ['dom_content_loaded_ms', timing.domContentLoadedEventEnd - timing.startTime],
      ['load_ms', timing.loadEventEnd - timing.startTime],
    ] as const) {
      if (Number.isFinite(value) && value >= 0 && value <= MAX_DURATION) attributes[`browser.document.${key}`] = value;
    }
    start('browser.document', attributes, SpanKind.INTERNAL, false)?.finish();
  }
  const loaded = () => { setTimeout(documentTiming, 0); };
  if (initialConsent) {
    if (document.readyState === 'complete') loaded();
    else window.addEventListener('load', loaded, { once: true });
  }
  refreshConsent();
  return {
    refreshConsent,
    beginNavigation(key, routeName, redirected) {
      cancelFrames();
      navigation?.measurement.finish(redirected ? 'redirected' : 'cancelled');
      navigation = undefined;
      group = groups.routeGroup(routeName);
      const measurement = start('browser.navigation', { 'browser.route.group': group }, SpanKind.INTERNAL, false);
      if (measurement) navigation = { key, measurement };
    },
    completeNavigation(key, outcome: NavigationOutcome) {
      if (navigation?.key !== key) return;
      const normalized: NavigationOutcome = ['complete', 'cancelled', 'redirected', 'error', 'timeout'].includes(outcome) ? outcome : 'error';
      const finish = () => {
        if (navigation?.key !== key) return;
        navigation.measurement.finish(normalized);
        navigation = undefined;
      };
      if (normalized !== 'complete') { cancelFrames(); finish(); return; }
      const owner = generation;
      void Promise.resolve().then(postRender).then(() => {
        if (disposed || generation !== owner || navigation?.key !== key) return;
        const first = requestAnimationFrame(() => {
          frames.delete(first);
          if (disposed || generation !== owner || navigation?.key !== key) return;
          const second = requestAnimationFrame(() => { frames.delete(second); finish(); });
          frames.add(second);
        });
        frames.add(first);
      }).catch(() => {
        if (!disposed && generation === owner && navigation?.key === key) { navigation.measurement.finish('error'); navigation = undefined; }
      });
    },
    recordError,
    startRequest(method, api) {
      const measurement = start('browser.request', {
        'browser.route.group': group, 'browser.api.group': groups.normalizeAPIGroup(api),
        'http.request.method': requestMethod(method),
      }, SpanKind.CLIENT);
      if (!measurement) return;
      const owner = generation;
      const context = measurement.span.spanContext();
      return {
        traceparent: `00-${context.traceId}-${context.spanId}-01`,
        valid() { refreshConsent(); return !measurement.done && generation === owner && !owner?.closed; },
        finish(outcome: RequestOutcome, status?: number) {
          const normalized: RequestOutcome = ['success', 'http-error', 'network-error', 'cancelled', 'timeout'].includes(outcome) ? outcome : 'network-error';
          measurement.finish(normalized, status);
        },
      };
    },
    async forceFlush() { refreshConsent(); await generation?.provider.forceFlush({ timeoutMillis: 25000 }); },
    async stop() {
      if (disposed) { await Promise.all(closing); return; }
      disposed = true;
      documentPending = false;
      closeGeneration();
      window.removeEventListener('error', windowError);
      window.removeEventListener('unhandledrejection', rejection);
      unsubscribe?.();
      window.removeEventListener('load', loaded);
      cleanupFramework?.();
      await Promise.all(closing);
    },
  };
}
