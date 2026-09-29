/**
 * Explicit first-party Axios propagation.
 *
 * Must not import the OpenTelemetry SDK. Telemetry access and API group
 * resolution are injected so multiple telemetry instances cannot cross-
 * contaminate, and hosts keep their own axios wiring.
 */
import { AxiosHeaders, type AxiosAdapter, type AxiosError, type AxiosInstance, type Cancel, type InternalAxiosRequestConfig } from 'axios';
import type { RequestMeasurement, RequestOutcome } from './types';
import type { RouteGroups } from './groups';

export type TelemetryGetter = () =>
| { startRequest(method: string, group: string): RequestMeasurement | undefined } | undefined;

/** Structural slice of the axios module this integration needs. */
export interface AxiosModule {
  getAdapter(adapters: InternalAxiosRequestConfig['adapter'], config: InternalAxiosRequestConfig): AxiosAdapter;
  isCancel(payload: unknown): payload is Cancel;
  isAxiosError(payload: unknown): payload is AxiosError;
}

type TracedConfig = InternalAxiosRequestConfig & { __browserTelemetry?: OwnedRequest };
const DISPATCH_OWNER = '__browserTelemetryDispatch';
type DispatchOptions = RequestInit & { [DISPATCH_OWNER]?: OwnedRequest };
// Axios preserves non-plain values while merging a reused error.config. Keep only
// this integration's ownership here; never remove headers supplied by the caller.
class OwnedRequest {
  groups!: RouteGroups;
  originalAdapter: InternalAxiosRequestConfig['adapter'];
  originalFetchOptions: InternalAxiosRequestConfig['fetchOptions'];
  wrapper?: AxiosAdapter;
  injected?: string;
  measurement?: RequestMeasurement;
  RequestConstructor?: typeof Request | null;
  constructor(config: TracedConfig) {
    this.originalAdapter = config.adapter;
    this.originalFetchOptions = config.fetchOptions;
    this.RequestConstructor = config.env?.Request === undefined ? globalThis.Request : config.env.Request;
  }
  restore(config: TracedConfig): void {
    if (this.injected && config.headers.get('traceparent') === this.injected) config.headers.delete('traceparent');
    if (config.adapter === this.wrapper) config.adapter = this.originalAdapter;
    if ((config.fetchOptions as DispatchOptions | undefined)?.[DISPATCH_OWNER] === this) {
      const { [DISPATCH_OWNER]: _owner, ...rest } = config.fetchOptions as DispatchOptions;
      config.fetchOptions = rest;
    }
    if (this.injected && config.fetchOptions?.redirect === 'error') {
      const { redirect: _redirect, ...rest } = config.fetchOptions;
      config.fetchOptions = { ...rest, ...(this.originalFetchOptions?.redirect ? { redirect: this.originalFetchOptions.redirect } : {}) };
    }
  }
}
const observed = new WeakMap<AxiosInstance, TelemetryGetter>();
const fetchGates = new WeakMap<typeof fetch, typeof fetch>();

function dispatchGate(send: typeof fetch): typeof fetch {
  const existing = fetchGates.get(send);
  if (existing) return existing;
  const gated: typeof fetch = (input, options) => {
    const { [DISPATCH_OWNER]: owner, ...forwarded } = (options ?? {}) as DispatchOptions;
    if (owner instanceof OwnedRequest) {
      const isRequest = owner.RequestConstructor && input instanceof owner.RequestConstructor;
      const uri = isRequest ? (input as Request).url : String(input);
      if (!owner.measurement?.valid() || !owner.groups.apiGroup(uri, window.location.origin)) {
        const redirect = owner.originalFetchOptions?.redirect ?? 'follow';
        forwarded.redirect = redirect;
        if (isRequest) {
          const request = input as Request;
          const headers = new Headers(request.headers);
          if (headers.get('traceparent') === owner.injected) headers.delete('traceparent');
          // Request copying retains its body stream, signal and credentials. Do
          // not reconstruct it from URL/body or cancel the business operation.
          input = new owner.RequestConstructor!(request, { headers, redirect });
        }
        if (forwarded.headers) {
          const headers = new Headers(forwarded.headers);
          if (headers.get('traceparent') === owner.injected) {
            headers.delete('traceparent');
            forwarded.headers = headers;
          }
        }
      } else {
        // fetchOptions is the final RequestInit and can override Request fields.
        forwarded.redirect = 'error';
      }
    }
    return send(input, forwarded);
  };
  // Axios caches adapter factories by env.fetch identity in a strong Map. Reuse
  // a stable gate; carry each request's ownership in an opaque init field.
  fetchGates.set(send, gated);
  return gated;
}

export interface ObserveAxiosOptions {
  /** Returns the telemetry instance (or undefined before startup). */
  telemetry: TelemetryGetter;
  /** Finite API group resolver built by createGroups. */
  groups: RouteGroups;
}

function outcomeOf(axiosModule: AxiosModule, error: unknown): RequestOutcome {
  if (axiosModule.isCancel(error)) return 'cancelled';
  if (axiosModule.isAxiosError(error)) {
    if (error.code === 'ETIMEDOUT' || error.code === 'ECONNABORTED') return 'timeout';
    if (error.response) return 'http-error';
  }
  return 'network-error';
}

/**
 * Installs consent-aware fetch adaptation on an Axios instance. The instance is
 * bounded to the injected telemetry getter, so distinct telemetry instances
 * never share state.
 */
export function observeAxiosInstance(axiosModule: AxiosModule, client: AxiosInstance, options: ObserveAxiosOptions): void {
  const key = observed.get(client);
  if (key || !client.interceptors?.request) return;
  observed.set(client, options.telemetry);
  // Axios 1.x's resolver accepts the request config for its env.fetch/Request
  // adapter factory; its public declaration currently omits that second argument.
  const resolveAdapter = axiosModule.getAdapter;
  const telemetryOf = options.telemetry;
  client.interceptors.request.use((incoming) => {
    const config = incoming as TracedConfig;
    config.headers = AxiosHeaders.from(config.headers);
    if (config.__browserTelemetry instanceof OwnedRequest) config.__browserTelemetry.restore(config);
    delete config.__browserTelemetry;
    if (!telemetryOf() || typeof config.adapter === 'function'
      || (Array.isArray(config.adapter) && config.adapter.some((adapter) => typeof adapter === 'function'))) return config;
    let normal: AxiosAdapter;
    try { normal = resolveAdapter(config.adapter, config); resolveAdapter('fetch', config); } catch { return config; }
    const owner = new OwnedRequest(config);
    owner.groups = options.groups;
    owner.wrapper = async (dispatched) => {
      const final = dispatched as TracedConfig;
      let group: string | undefined;
      try { group = options.groups.apiGroup(client.getUri(final), window.location.origin); } catch { /* Invalid URLs retain normal Axios behavior. */ }
      if (!group || final.headers.has('traceparent') || Object.hasOwn(final.fetchOptions ?? {}, DISPATCH_OWNER)) return normal(final);
      const measurement = telemetryOf()?.startRequest(final.method ?? 'GET', group);
      if (!measurement || !measurement.valid()) return normal(final);
      owner.measurement = measurement;
      owner.injected = measurement.traceparent;
      final.headers.set('traceparent', owner.injected);
      final.fetchOptions = { ...final.fetchOptions, redirect: 'error', [DISPATCH_OWNER]: owner } as DispatchOptions;
      try {
        const send = final.env?.fetch ?? globalThis.fetch;
        const fetchAdapter = resolveAdapter('fetch', { ...final, env: { ...final.env, fetch: dispatchGate(send) } });
        const response = await fetchAdapter(final);
        measurement.finish(response.status >= 400 ? 'http-error' : 'success', response.status);
        return response;
      } catch (error) {
        // Axios performs its final cancellation check after the adapter rejects.
        const result = final.signal?.aborted || final.cancelToken?.reason ? 'cancelled' : outcomeOf(axiosModule, error);
        measurement.finish(result, axiosModule.isAxiosError(error) ? error.response?.status : undefined);
        throw error;
      }
    };
    config.__browserTelemetry = owner;
    config.adapter = owner.wrapper;
    return config;
  });
}
