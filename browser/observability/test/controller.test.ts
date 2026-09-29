import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createApp, nextTick } from 'vue';
import { context, trace } from '@opentelemetry/api';
import { createVueIntegration } from '../src/vue';
import { createBrowserTelemetry } from '../src/controller';
import type { BrowserTelemetry, ConsentSource } from '../src/types';
import { syntheticConsent, type SyntheticConsent } from './consent';
import { GROUPS, INTAKE_PATH } from './groups';
import { createGroups } from '../src/groups';

type WireSpan = { name: string; traceId: string; spanId: string; parentSpanId?: string; startTimeUnixNano: string; endTimeUnixNano: string; attributes: Array<{ key: string; value: { stringValue?: string; intValue?: number; doubleValue?: number } }>; status: { code: number } };
const attributes = (span: WireSpan) => Object.fromEntries(span.attributes.map(({ key, value }) => [key, value.stringValue ?? value.intValue ?? value.doubleValue]));
const spans = (send: ReturnType<typeof vi.fn>): WireSpan[] => (send.mock.calls as unknown[][]).flatMap(([, init]) => JSON.parse((init as RequestInit).body as string).resourceSpans.flatMap((resource: any) => resource.scopeSpans.flatMap((scope: any) => scope.spans)));
let telemetry: BrowserTelemetry | undefined;
const make = (send: (url: unknown, init?: RequestInit) => Promise<Response>, consent: ConsentSource | SyntheticConsent = syntheticConsent(true), extra: Partial<Parameters<typeof createBrowserTelemetry>[0]> = {}) =>
  createBrowserTelemetry({ consent, changes: 'changes' in consent ? consent.changes : undefined, serviceName: 'host-web', scope: 'example.com/host', groups: GROUPS, intakePath: INTAKE_PATH, fetch: send as unknown as typeof fetch, ...extra });

describe('consent-scoped browser telemetry', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(async () => { await telemetry?.stop(); telemetry = undefined; vi.useRealTimers(); vi.restoreAllMocks(); });

  it('serializes native parentage and finite attributes without installing globals or leaking error/route values', async () => {
    const beforeProvider = trace.getTracerProvider();
    const beforeContext = context.active();
    const send = vi.fn().mockResolvedValue({ status: 202 });
    telemetry = make(send);
    const navigation = {};
    telemetry.beginNavigation(navigation, 'settings', false);
    const request = telemetry.startRequest('POST', 'accounts')!;
    request.finish('success', 201);
    telemetry.recordError(new TypeError('private-prompt-and-url'), 'vue');
    telemetry.completeNavigation(navigation, 'complete');
    await nextTick();
    await vi.advanceTimersByTimeAsync(40);
    await telemetry.forceFlush();
    const emitted = spans(send);
    expect(emitted.map((span) => span.name).sort()).toEqual(['browser.error', 'browser.navigation', 'browser.request']);
    const nav = emitted.find((span) => span.name === 'browser.navigation')!;
    const req = emitted.find((span) => span.name === 'browser.request')!;
    expect(req.traceId).toBe(nav.traceId);
    expect(req.parentSpanId).toBe(nav.spanId);
    expect(request.traceparent).toBe(`00-${req.traceId}-${req.spanId}-01`);
    expect(attributes(req)).toEqual({ 'browser.route.group': 'settings', 'browser.api.group': 'accounts', 'http.request.method': 'POST', 'browser.outcome': 'success', 'http.response.status_code': 201 });
    for (const span of emitted) {
      expect(span.traceId).toMatch(/^[a-f0-9]{32}$/);
      expect(span.spanId).toMatch(/^[a-f0-9]{16}$/);
      expect(span.startTimeUnixNano).toMatch(/^\d+$/);
    }
    expect(JSON.stringify(send.mock.calls)).not.toContain('private-');
    expect(send.mock.calls[0][0]).toBe(INTAKE_PATH);
    expect(send.mock.calls[0][1]).toMatchObject({ credentials: 'omit', redirect: 'error', mode: 'same-origin', keepalive: false, headers: { 'Content-Type': 'application/json' } });
    expect(trace.getTracerProvider()).toBe(beforeProvider);
    expect(context.active()).toBe(beforeContext);
    const resource = JSON.parse(send.mock.calls[0][1].body).resourceSpans[0].resource.attributes;
    expect(Object.fromEntries(resource.map((attribute: any) => [attribute.key, attribute.value.stringValue]))['service.name']).toBe('host-web');
  });

  it('drops queued/active work synchronously on revoke and creates fresh IDs after regrant', async () => {
    const send = vi.fn().mockResolvedValue({ status: 202 });
    const consent = syntheticConsent(false);
    telemetry = make(send, consent);
    expect(telemetry.startRequest('GET', 'accounts')).toBeUndefined();
    consent.grant();
    telemetry.refreshConsent();
    const pending = telemetry.startRequest('GET', 'accounts')!;
    telemetry.recordError(new Error('private-before-revoke'), 'window');
    consent.revoke();
    expect(pending.valid()).toBe(false);
    pending.finish('success', 200);
    await telemetry.forceFlush();
    expect(send).not.toHaveBeenCalled();
    consent.grant();
    telemetry.refreshConsent();
    const fresh = telemetry.startRequest('GET', 'accounts')!;
    fresh.finish('success', 200);
    await telemetry.forceFlush();
    expect(spans(send)).toHaveLength(1);
    expect(fresh.traceparent).not.toBe(pending.traceparent);
  });

  it('aborts an in-flight export and prevents stale callbacks from reviving a revoked generation', async () => {
    let signal: AbortSignal | null | undefined;
    const send = vi.fn((_url: unknown, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      signal = init?.signal ?? null;
      signal?.addEventListener('abort', () => reject(new Error('private-downstream-body')));
    }));
    const consent = syntheticConsent(true);
    telemetry = make(send, consent);
    telemetry.recordError('private-string-rejection', 'unhandledrejection');
    const flush = telemetry.forceFlush();
    await vi.advanceTimersByTimeAsync(0);
    expect(signal?.aborted).toBe(false);
    consent.revoke();
    expect(signal?.aborted).toBe(true);
    await flush;
    expect(telemetry.startRequest('GET', 'accounts')).toBeUndefined();
  });

  it('bounds suspended measurements and active work while cancelling obsolete render callbacks', async () => {
    const send = vi.fn().mockResolvedValue({ status: 202 });
    telemetry = make(send);
    const old = {};
    telemetry.beginNavigation(old, 'private-route-id', false);
    telemetry.completeNavigation(old, 'complete');
    await vi.advanceTimersByTimeAsync(0);
    const cancel = vi.spyOn(globalThis, 'cancelAnimationFrame');
    const next = {};
    telemetry.beginNavigation(next, 'app', true);
    expect(cancel).toHaveBeenCalled();
    telemetry.completeNavigation(old, 'complete');
    const requests = Array.from({ length: 128 }, () => telemetry!.startRequest('PRIVATE-METHOD', 'other'));
    expect(requests.filter(Boolean)).toHaveLength(127);
    vi.setSystemTime(Date.now() + 180000);
    requests[0]!.finish('success', 200);
    telemetry.completeNavigation(next, 'cancelled');
    await telemetry.forceFlush();
    const emitted = spans(send);
    expect(emitted.filter((span) => span.name === 'browser.navigation').map(attributes)).toEqual([
      { 'browser.route.group': 'other', 'browser.outcome': 'redirected' },
      { 'browser.route.group': 'home', 'browser.outcome': 'cancelled' },
    ]);
    for (const span of emitted) expect(BigInt(span.endTimeUnixNano) - BigInt(span.startTimeUnixNano)).toBeLessThanOrEqual(120000000000n);
    expect(attributes(emitted.find((span) => span.name === 'browser.request')!)['http.request.method']).toBe('OTHER');
  });

  it('chains Vue handlers with identical arguments and restores them on stop', async () => {
    const original = vi.fn();
    const app = createApp({});
    app.config.errorHandler = original;
    const send = vi.fn().mockResolvedValue({ status: 202 });
    telemetry = make(send, syntheticConsent(true), { framework: createVueIntegration(app) });
    const error = new ReferenceError('private-component-message');
    app.config.errorHandler!(error, null, 'private-component-info');
    expect(original).toHaveBeenCalledWith(error, null, 'private-component-info');
    await telemetry.forceFlush();
    expect(attributes(spans(send)[0])['error.type']).toBe('reference-error');
    expect(JSON.stringify(send.mock.calls)).not.toContain('private-');
    await telemetry.stop();
    expect(app.config.errorHandler).toBe(original);
  });

  it('exports only initial-consent document timing with finite numeric values and no URL', async () => {
    const original = Object.getOwnPropertyDescriptor(performance, 'getEntriesByType');
    Object.defineProperty(performance, 'getEntriesByType', { configurable: true, value: () => [{
      name: 'https://private-host/private-path?prompt=private-prompt', startTime: 0,
      requestStart: 5, responseStart: 17.5, domContentLoadedEventEnd: 80, loadEventEnd: 120,
    }] });
    const send = vi.fn().mockResolvedValue({ status: 202 });
    try {
      telemetry = make(send);
      window.dispatchEvent(new Event('load'));
      await vi.advanceTimersByTimeAsync(1);
      await telemetry.forceFlush();
      const document = spans(send).filter((span) => span.name === 'browser.document');
      expect(document).toHaveLength(1);
      expect(attributes(document[0])).toEqual({ 'browser.route.group': 'other', 'browser.document.ttfb_ms': 12.5, 'browser.document.dom_content_loaded_ms': 80, 'browser.document.load_ms': 120 });
      expect(JSON.stringify(send.mock.calls)).not.toContain('private-');
      await telemetry.stop();
      send.mockClear();
      let allowed = false;
      telemetry = make(send, { isAllowed: () => allowed });
      allowed = true;
      telemetry.refreshConsent();
      window.dispatchEvent(new Event('load'));
      await vi.advanceTimersByTimeAsync(1);
      await telemetry.forceFlush();
      expect(send).not.toHaveBeenCalled();
    } finally {
      if (original) Object.defineProperty(performance, 'getEntriesByType', original);
      else Reflect.deleteProperty(performance, 'getEntriesByType');
    }
  });

  it('closes in-flight export at host-supplied consent expiry', async () => {
    const consent = syntheticConsent(true);
    consent.grant(Date.now() + 20);
    let signal: AbortSignal | null | undefined;
    const send = vi.fn((_url: unknown, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      signal = init?.signal ?? null;
      signal?.addEventListener('abort', () => reject(new Error('aborted')));
    }));
    telemetry = make(send, consent);
    telemetry.recordError(new Error(), 'window');
    const flush = telemetry.forceFlush();
    await vi.advanceTimersByTimeAsync(1);
    expect(signal?.aborted).toBe(false);
    await vi.advanceTimersByTimeAsync(20);
    expect(signal?.aborted).toBe(true);
    await flush;
  });

  it('subscribes to the host change bus and revokes via notification', async () => {
    const consent = syntheticConsent(true);
    const send = vi.fn().mockResolvedValue({ status: 202 });
    telemetry = make(send, consent, { changes: consent.changes });
    const active = telemetry.startRequest('GET', 'other')!;
    consent.revoke();
    expect(active.valid()).toBe(false);
  });

  it('keeps two controller instances isolated without cross-contamination', async () => {
    const sendA = vi.fn().mockResolvedValue({ status: 202 });
    const sendB = vi.fn().mockResolvedValue({ status: 202 });
    const consentA = syntheticConsent(true);
    const consentB = syntheticConsent(true);
    const a = make(sendA, consentA);
    const b = make(sendB, consentB);
    try {
      const requestA = a.startRequest('GET', 'accounts')!;
      const requestB = b.startRequest('GET', 'accounts')!;
      expect(requestA.traceparent).not.toBe(requestB.traceparent);
      consentB.revoke();
      expect(requestB.valid()).toBe(false);
      expect(requestA.valid()).toBe(true);
      requestA.finish('success', 200);
      await a.forceFlush();
      await b.forceFlush();
      expect(sendA).toHaveBeenCalled();
      expect(sendB).not.toHaveBeenCalled();
    } finally {
      await a.stop();
      await b.stop();
    }
  });

  it('rejects invalid group configuration eagerly', () => {
    expect(() => createGroups({ routes: {}, apis: [['relative/path', 'x']] })).toThrow(TypeError);
    expect(() => createGroups({ routes: { ok: '' }, apis: [] })).toThrow(TypeError);
    expect(() => createGroups({ routes: {}, apis: [['/api/v1/x', '']] })).toThrow(TypeError);
    expect(() => createGroups({ routes: {}, apis: [], telemetryPaths: ['relative'] })).toThrow(TypeError);
  });

  it('uses only configured finite vocabulary for routes, APIs and methods', async () => {
    const groups = createGroups(GROUPS);
    expect(groups.routeGroup('settings')).toBe('settings');
    expect(groups.routeGroup('private-route-name')).toBe('other');
    const origin = window.location.origin;
    expect(groups.apiGroup('/api/v1/ums/private-id?prompt=private-prompt', origin)).toBe('accounts');
    expect(groups.apiGroup('/api/v1/umsalias', origin)).toBe('other');
    for (const uri of ['https://foreign.invalid/api/v1/ums', INTAKE_PATH, '/api/v1/%74elemetry/browser/traces', '/api/v1/ums/%2e%2e/telemetry', '/api//v1/ums', '//foreign.invalid/api/v1/ums', '/not-api', 'https://user:password@localhost/api/v1/ums']) {
      expect(groups.apiGroup(uri, origin)).toBeUndefined();
    }
    const { requestMethod } = await import('../src/groups');
    expect(requestMethod('get')).toBe('GET');
    expect(requestMethod('PRIVATE-METHOD')).toBe('OTHER');
  });

  it.each([undefined, NaN, Infinity, -Infinity, 0])('fails closed for an invalid expiry (%s) even when isAllowed stays true', async (expiresAt) => {
    const send = vi.fn().mockResolvedValue({ status: 202 });
    const getExpiresAt = vi.fn(() => expiresAt);
    telemetry = make(send, { isAllowed: () => true, getExpiresAt });
    expect(telemetry.startRequest('GET', 'accounts')).toBeUndefined();
    const reads = getExpiresAt.mock.calls.length;
    await vi.advanceTimersByTimeAsync(100);
    expect(getExpiresAt).toHaveBeenCalledTimes(reads);
    await telemetry.forceFlush();
    expect(send).not.toHaveBeenCalled();
  });

  it('closes synchronously if expiry retrieval throws, without surfacing storage failures', async () => {
    const send = vi.fn().mockResolvedValue({ status: 202 });
    let blocked = false;
    telemetry = make(send, {
      isAllowed: () => true,
      getExpiresAt: () => { if (blocked) throw new Error('private-storage-value'); return Date.now() + 1000; },
    });
    const request = telemetry.startRequest('GET', 'accounts')!;
    telemetry.recordError(new Error(), 'window');
    blocked = true;
    expect(() => telemetry!.refreshConsent()).not.toThrow();
    expect(request.valid()).toBe(false);
    request.finish('success');
    await telemetry.forceFlush();
    expect(send).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(10000);
    expect(telemetry.startRequest('GET', 'accounts')).toBeUndefined();
  });

  it('discards queued and active spans at expiry even if the host boolean remains true', async () => {
    const send = vi.fn().mockResolvedValue({ status: 202 });
    const getExpiresAt = vi.fn(() => expiresAt);
    const expiresAt = Date.now() + 20;
    telemetry = make(send, { isAllowed: () => true, getExpiresAt });
    const request = telemetry.startRequest('GET', 'accounts')!;
    telemetry.recordError(new Error(), 'window');
    await vi.advanceTimersByTimeAsync(20);
    expect(request.valid()).toBe(false);
    const reads = getExpiresAt.mock.calls.length;
    await vi.advanceTimersByTimeAsync(10000);
    expect(getExpiresAt).toHaveBeenCalledTimes(reads);
    request.finish('success');
    await telemetry.forceFlush();
    expect(send).not.toHaveBeenCalled();
  });

  it('normalizes untyped API groups and outcomes, and drops unknown error sources', async () => {
    const send = vi.fn().mockResolvedValue({ status: 202 });
    telemetry = make(send);
    const key = {};
    telemetry.beginNavigation(key, 'private-route', false);
    const request = telemetry.startRequest('private-method', 'private-api-id')!;
    request.finish('private-outcome' as never, 200);
    telemetry.completeNavigation(key, 'private-navigation-outcome' as never);
    telemetry.recordError(new Error('private-message'), 'private-source' as never);
    await telemetry.forceFlush();
    const emitted = spans(send);
    expect(emitted).toHaveLength(2);
    expect(attributes(emitted.find((span) => span.name === 'browser.request')!)).toEqual({
      'browser.route.group': 'other', 'browser.api.group': 'other', 'http.request.method': 'OTHER',
      'browser.outcome': 'network-error', 'http.response.status_code': 200,
    });
    expect(attributes(emitted.find((span) => span.name === 'browser.navigation')!)['browser.outcome']).toBe('error');
    expect(JSON.stringify(send.mock.calls)).not.toContain('private-');
  });

  it('uses framework hooks without importing a framework and cleans up on stop', async () => {
    const send = vi.fn().mockResolvedValue({ status: 202 });
    const cleanup = vi.fn();
    const postRender = vi.fn().mockResolvedValue(undefined);
    let record!: BrowserTelemetry['recordError'];
    telemetry = make(send, syntheticConsent(true), { framework: {
      subscribeErrors(recorder) { record = recorder; return cleanup; }, postRender,
    } });
    record(new TypeError('private-message'), 'window');
    const key = {};
    telemetry.beginNavigation(key, 'settings', false);
    telemetry.completeNavigation(key, 'complete');
    await vi.advanceTimersByTimeAsync(40);
    expect(postRender).toHaveBeenCalledTimes(1);
    await telemetry.forceFlush();
    expect(spans(send).map((span) => span.name).sort()).toEqual(['browser.error', 'browser.navigation']);
    await telemetry.stop();
    expect(cleanup).toHaveBeenCalledTimes(1);
    await telemetry.stop();
    expect(cleanup).toHaveBeenCalledTimes(1);
  });

});
