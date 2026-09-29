import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import axios, { AxiosError, type AxiosInstance } from 'axios';
import { createBrowserTelemetry } from '../src/controller';
import { observeAxiosInstance } from '../src/axios';
import { createGroups } from '../src/groups';
import { setBrowserTelemetry, type BrowserTelemetry } from '../src/facade';
import { syntheticConsent, type SyntheticConsent } from './consent';
import { GROUPS, INTAKE_PATH } from './groups';

let telemetry: BrowserTelemetry;
let consent: SyntheticConsent;
let intake: ReturnType<typeof vi.fn>;
const businessFetch = vi.fn();
beforeEach(() => {
  consent = syntheticConsent(true);
  intake = vi.fn().mockResolvedValue({ status: 202 });
  telemetry = createBrowserTelemetry({ consent, serviceName: 'host-web', scope: 'example.com/host', groups: GROUPS, intakePath: INTAKE_PATH, fetch: intake as typeof fetch });
  businessFetch.mockReset().mockImplementation(async () => new Response('{"ok":true}', { status: 200, headers: { 'Content-Type': 'application/json' } }));
});
afterEach(async () => { setBrowserTelemetry(undefined); await telemetry.stop(); vi.restoreAllMocks(); });
const client = (getter: () => BrowserTelemetry | undefined = () => telemetry): AxiosInstance => {
  const instance = axios.create({ env: { fetch: businessFetch } });
  instance.defaults.baseURL = `${window.location.origin}/api/v1/ums`;
  instance.defaults.withCredentials = true;
  observeAxiosInstance(axios, instance, { telemetry: getter, groups: createGroups(GROUPS) });
  return instance;
};

describe('explicit first-party Axios propagation', () => {
  it('uses real Axios fetch adaptation with unchanged body, credentials and response', async () => {
    const instance = client();
    const response = await instance.post('/lookup?registration=private-registration', { prompt: 'private-prompt' });
    expect(response.status).toBe(200);
    expect(response.data).toEqual({ ok: true });
    const request = businessFetch.mock.calls[0][0] as Request;
    expect(request.redirect).toBe('error');
    expect(request.credentials).toBe('include');
    expect(request.headers.get('traceparent')).toMatch(/^00-[a-f0-9]{32}-[a-f0-9]{16}-01$/);
    expect(await request.clone().json()).toEqual({ prompt: 'private-prompt' });
    await telemetry.forceFlush();
    expect(JSON.stringify(intake.mock.calls)).not.toContain('private-');
    expect(JSON.stringify(intake.mock.calls)).toContain('accounts');
  });

  it('retains Axios cancellation and timeout behavior', async () => {
    const instance = client();
    // jsdom's AbortSignal and Node's Request have different brands. Axios also
    // supports fetch(url, init), which keeps its real signal composition here.
    businessFetch.mockImplementation((_url, init: RequestInit) => new Promise((_resolve, reject) => {
      init.signal!.addEventListener('abort', () => reject(init.signal!.reason));
    }));
    const controller = new AbortController();
    const withoutRequest = { Request: null as unknown as typeof Request };
    const cancelled = instance.get('/cancel', { signal: controller.signal, env: withoutRequest }).catch((error) => error);
    await new Promise((resolve) => setTimeout(resolve, 10));
    controller.abort();
    const cancellationResult = await cancelled;
    expect(axios.isCancel(cancellationResult)).toBe(true);
    const timeout = await instance.get('/timeout', { timeout: 10, env: withoutRequest }).catch((error) => error);
    expect(timeout.code).toBe('ETIMEDOUT');
    await telemetry.forceFlush();
    const bodies = JSON.stringify(intake.mock.calls);
    expect(bodies).toContain('cancelled');
    expect(bodies).toContain('timeout');
  });

  it('rechecks consent after asynchronous upload preparation and preserves the streamed business request', async () => {
    let prepared!: () => void;
    let release!: () => void;
    const preparing = new Promise<void>((resolve) => { prepared = resolve; });
    const released = new Promise<void>((resolve) => { release = resolve; });
    class DelayedRequest extends Request {
      override async arrayBuffer(): Promise<ArrayBuffer> {
        prepared();
        await released;
        return super.arrayBuffer();
      }
    }
    const instance = client();
    // Keep Request/FormData in the same native fetch realm; jsdom's FormData
    // would be stringified by Node's Request instead of streaming its fields.
    const form = await new Response('prompt=private-upload-content', {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    }).formData();
    const pending = instance.post('/upload', form, {
      onUploadProgress: vi.fn(), env: { Request: DelayedRequest },
      fetchOptions: { credentials: 'same-origin', headers: { 'X-Caller': 'preserved' } },
    });
    await preparing;
    expect(businessFetch).not.toHaveBeenCalled();
    consent.revoke();
    release();
    const response = await pending;
    expect(response.data).toEqual({ ok: true });
    const [request, init] = businessFetch.mock.calls[0] as [Request, RequestInit];
    expect(request.headers.has('traceparent')).toBe(false);
    expect(request.method).toBe('POST');
    expect(request.credentials).toBe('include');
    expect(request.signal.aborted).toBe(false);
    expect(request.redirect).toBe('follow');
    expect(await request.text()).toContain('private-upload-content');
    expect(init).toMatchObject({ credentials: 'same-origin', redirect: 'follow', headers: { 'X-Caller': 'preserved' } });
    expect(Object.keys(init)).not.toContain('__browserTelemetryDispatch');
    await telemetry.forceFlush();
    expect(intake).not.toHaveBeenCalled();
  });

  it('removes its header from fetch(url, init) if consent changes during signal preparation', async () => {
    const instance = client();
    const source = axios.CancelToken.source();
    const token = source.token as typeof source.token & { toAbortSignal(): AbortSignal };
    const original = token.toAbortSignal.bind(token);
    vi.spyOn(token, 'toAbortSignal').mockImplementation(() => {
      consent.revoke();
      return original();
    });
    await instance.post('/upload', { prompt: 'private-upload-content' }, {
      cancelToken: source.token, env: { Request: null as unknown as typeof Request },
    });
    const [url, init] = businessFetch.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('/api/v1/ums/upload');
    expect(new Headers(init.headers).has('traceparent')).toBe(false);
    expect(init.body).toBe('{"prompt":"private-upload-content"}');
    expect(init.signal?.aborted).toBe(false);
    expect(init.redirect).toBe('follow');
    expect(Object.keys(init)).not.toContain('__browserTelemetryDispatch');
    await telemetry.forceFlush();
    expect(intake).not.toHaveBeenCalled();
  });

  it('cleans owned headers and adapter policy from a reused failed request after revoke or foreign retargeting', async () => {
    const instance = client();
    businessFetch.mockResolvedValueOnce(new Response('{"error":"private-response"}', { status: 503, headers: { 'Content-Type': 'application/json' } }));
    let failure: AxiosError | undefined;
    try { await instance.get('/failure', { adapter: 'fetch' }); } catch (error) { failure = error as AxiosError; }
    expect(failure?.response?.status).toBe(503);
    expect(failure?.response?.data).toEqual({ error: 'private-response' });
    const config = failure!.config!;
    expect(config.headers.get('traceparent')).toBeTruthy();
    consent.revoke();
    await instance.request(config);
    const afterRevoke = businessFetch.mock.calls[1][0] as Request;
    expect(afterRevoke.headers.has('traceparent')).toBe(false);
    expect(afterRevoke.redirect).toBe('follow');
    consent.grant();
    config.url = 'https://foreign.invalid/api/v1/ums/failure';
    await instance.request(config);
    const foreign = businessFetch.mock.calls[2][0] as Request;
    expect(foreign.headers.has('traceparent')).toBe(false);
    expect(foreign.redirect).toBe('follow');
  });

  it('leaves caller trace headers and custom adapters untouched and preserves error identity', async () => {
    const instance = client();
    const sameError = new AxiosError('private-existing-adapter-error');
    const adapter = vi.fn().mockRejectedValue(sameError);
    await expect(instance.get('/custom', { adapter })).rejects.toBe(sameError);
    expect(adapter.mock.calls[0][0].headers.has('traceparent')).toBe(false);
    await instance.get('/caller', { adapter: 'fetch', headers: { traceparent: 'caller-owned-value' } });
    const request = businessFetch.mock.calls[0][0] as Request;
    expect(request.headers.get('traceparent')).toBe('caller-owned-value');
    expect(request.redirect).toBe('follow');
    await telemetry.forceFlush();
    expect(intake).not.toHaveBeenCalled();
  });

  it('cleans its private retry state when the caller changes redirect policy', async () => {
    const instance = client();
    businessFetch.mockResolvedValueOnce(new Response('', { status: 503 }));
    const error = await instance.get('/retry', { adapter: 'fetch' }).catch((failure) => failure as AxiosError);
    const config = error.config!;
    const originalHeader = config.headers.get('traceparent');
    config.fetchOptions!.redirect = 'manual';
    await instance.request(config);
    const [request, init] = businessFetch.mock.calls[1] as [Request, RequestInit];
    expect(request.headers.get('traceparent')).toMatch(/^00-[a-f0-9]{32}-[a-f0-9]{16}-01$/);
    expect(request.headers.get('traceparent')).not.toBe(originalHeader);
    expect(init.redirect).toBe('error');
    expect(Object.keys(init)).not.toContain('__browserTelemetryDispatch');
  });

  it('binds each axios instance to its injected telemetry getter without cross-contamination', async () => {
    const other = createBrowserTelemetry({ consent: syntheticConsent(true), serviceName: 'other-web', scope: 'example.com/other', groups: GROUPS, intakePath: INTAKE_PATH, fetch: intake as typeof fetch });
    const foreign = vi.fn().mockResolvedValue({ status: 202 });
    try {
      const sharedFetch = vi.fn(async () => new Response('{"ok":true}', { status: 200, headers: { 'Content-Type': 'application/json' } }));
      const instanceA = axios.create({ baseURL: `${window.location.origin}/api/v1/ums`, env: { fetch: sharedFetch } });
      const instanceB = axios.create({ baseURL: `${window.location.origin}/api/v1/ums`, env: { fetch: sharedFetch } });
      observeAxiosInstance(axios, instanceA, { telemetry: () => telemetry, groups: createGroups(GROUPS) });
      observeAxiosInstance(axios, instanceB, { telemetry: () => other, groups: createGroups(GROUPS) });
      await instanceA.get('/a');
      await instanceB.get('/b');
      await telemetry.forceFlush();
      await other.forceFlush();
      expect(JSON.stringify(intake.mock.calls)).toContain('accounts');
      expect(JSON.stringify(foreign.mock.calls)).not.toContain('traceparent');
      // Both instances traced through their own injected telemetry.
      const requests = (sharedFetch.mock.calls as unknown[][]).map((call) => call[0] as unknown as Request);
      expect(requests).toHaveLength(2);
      for (const request of requests) expect(request.headers.get('traceparent')).toMatch(/^00-[a-f0-9]{32}-[a-f0-9]{16}-01$/);
      const traceIds = requests.map((request) => request.headers.get('traceparent')!.split('-')[1]);
      expect(new Set(traceIds).size).toBe(2);
    } finally {
      await other.stop();
    }
  });
});
