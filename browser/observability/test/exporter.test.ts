import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { resourceFromAttributes } from '@opentelemetry/resources';
import { BatchSpanProcessor, TracerProvider, type ReadableSpan, type SpanExporter } from '@opentelemetry/sdk-trace';
import { ExportResultCode } from '@opentelemetry/core';
import { consentExporter } from '../src/exporter';
import { INTAKE_PATH } from './groups';

let provider: TracerProvider | undefined;
let exporter: ReturnType<typeof consentExporter> | undefined;
beforeEach(() => { vi.useFakeTimers(); });
afterEach(async () => { exporter?.close(); await provider?.shutdown(); provider = undefined; vi.useRealTimers(); });
const ids = (send: { mock: { calls: unknown[][] } }): string[] => send.mock.calls.flatMap(([, init]) => JSON.parse((init as RequestInit).body as string).resourceSpans.flatMap((resource: any) => resource.scopeSpans.flatMap((scope: any) => scope.spans.map((span: any) => span.spanId))));
function sdk(send: typeof fetch, callbackCounts = new Map<string, number>(), captured: ReadableSpan[] = []) {
  exporter = consentExporter(() => true, send, INTAKE_PATH);
  const observed: SpanExporter = {
    export(spans, callback) {
      captured.push(...spans);
      const id = spans[0].spanContext().spanId;
      exporter!.export(spans, (result) => { callbackCounts.set(id, (callbackCounts.get(id) ?? 0) + 1); callback(result); });
    },
    shutdown: () => exporter!.shutdown(),
  };
  provider = new TracerProvider({ resource: resourceFromAttributes({ 'service.name': 'browser' }), spanProcessors: [new BatchSpanProcessor({
    exporter: observed, maxQueueSize: 128, maxExportBatchSize: 16, scheduledDelayMillis: 5000, exportTimeoutMillis: 25000,
  })] });
  const tracer = provider.getTracer('browser.telemetry');
  return (count: number) => Array.from({ length: count }, () => {
    const span = tracer.startSpan('browser.error');
    const id = span.spanContext().spanId;
    span.end();
    return id;
  });
}

describe('bounded serial OTLP export', () => {
  it('exports both ordinary consecutive batches without dropping the callback-scheduled second batch', async () => {
    let release!: (response: Response) => void;
    const first = new Promise<Response>((resolve) => { release = resolve; });
    const send = vi.fn().mockResolvedValue(new Response(null, { status: 202 })).mockReturnValueOnce(first);
    const ended = sdk(send as typeof fetch)(32);
    await vi.advanceTimersByTimeAsync(0);
    expect(send).toHaveBeenCalledTimes(1);
    release(new Response(null, { status: 202 }));
    await vi.advanceTimersByTimeAsync(0);
    expect(ids(send).sort()).toEqual(ended.sort());
    expect(new Set(ids(send)).size).toBe(32);
  });

  it('drains every ID from 128 spans during the SDK concurrent forceFlush with one fetch at a time', async () => {
    let release!: (response: Response) => void;
    const first = new Promise<Response>((resolve) => { release = resolve; });
    let inFlight = 0;
    let maximum = 0;
    let calls = 0;
    const send = vi.fn(() => {
      inFlight += 1;
      maximum = Math.max(maximum, inFlight);
      return (++calls === 1 ? first : Promise.resolve(new Response(null, { status: 202 }))).finally(() => { inFlight -= 1; });
    });
    const callbacks = new Map<string, number>();
    const ended = sdk(send as typeof fetch, callbacks)(128);
    const flush = provider!.forceFlush();
    await vi.advanceTimersByTimeAsync(0);
    expect(send).toHaveBeenCalledTimes(1);
    release(new Response(null, { status: 202 }));
    await flush;
    expect(maximum).toBe(1);
    expect(ids(send).sort()).toEqual(ended.sort());
    expect(new Set(ids(send)).size).toBe(128);
    expect([...callbacks.values()]).toEqual(Array(8).fill(1));
  });

  it('settles active and queued batches once on revoke, even after a late network completion', async () => {
    let release!: (response: Response) => void;
    const send = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) => new Promise<Response>((resolve) => { release = resolve; }));
    const callbacks = new Map<string, number>();
    sdk(send as typeof fetch, callbacks)(128);
    const flush = provider!.forceFlush();
    await vi.advanceTimersByTimeAsync(0);
    expect(send).toHaveBeenCalledTimes(1);
    const signal = send.mock.calls[0][1]!.signal as AbortSignal;
    exporter!.close();
    expect(signal.aborted).toBe(true);
    await flush;
    expect([...callbacks.values()]).toEqual(Array(8).fill(1));
    release(new Response(null, { status: 202 }));
    await vi.advanceTimersByTimeAsync(0);
    expect(send).toHaveBeenCalledTimes(1);
    expect([...callbacks.values()]).toEqual(Array(8).fill(1));
  });

  it('allows the bounded serial drain to finish when every transport takes almost three seconds', async () => {
    const send = vi.fn(() => new Promise<Response>((resolve) => {
      setTimeout(() => resolve(new Response(null, { status: 202 })), 2800);
    }));
    const ended = sdk(send as typeof fetch)(128);
    const flush = provider!.forceFlush({ timeoutMillis: 25000 });
    await vi.advanceTimersByTimeAsync(22500);
    await flush;
    expect(ids(send).sort()).toEqual(ended.sort());
  });

  it('reports overflow instead of acknowledging a ninth pending batch', async () => {
    const send = vi.fn(() => new Promise<Response>(() => {}));
    const captured: ReadableSpan[] = [];
    sdk(send as typeof fetch, new Map(), captured)(128);
    const flush = provider!.forceFlush();
    await vi.advanceTimersByTimeAsync(0);
    const overflow = vi.fn();
    exporter!.export(captured.slice(0, 16), overflow);
    expect(overflow).toHaveBeenCalledTimes(1);
    expect(overflow).toHaveBeenCalledWith({ code: ExportResultCode.FAILED });
    expect(send).toHaveBeenCalledTimes(1);
    exporter!.close();
    await flush;
    expect(overflow).toHaveBeenCalledTimes(1);
  });

  it('uses the configured same-origin intake path with strict transport options', async () => {
    const send = vi.fn().mockResolvedValue(new Response(null, { status: 202 }));
    sdk(send as typeof fetch)(1);
    await vi.advanceTimersByTimeAsync(5000);
    expect(send.mock.calls[0][0]).toBe(INTAKE_PATH);
    expect(send.mock.calls[0][1]).toMatchObject({ credentials: 'omit', redirect: 'error', mode: 'same-origin', keepalive: false, headers: { 'Content-Type': 'application/json' } });
  });

  it.each(['https://foreign.invalid/intake', '//foreign.invalid/intake', '/api//intake', '/api/%74elemetry', '/api/../intake', '/api/./intake', '/api/intake?private=value', '/api/intake#private', '/api/with space', '/api/with\\slash', '/api/with\nnewline', '/api/with\u0000null'])('rejects invalid intake configuration before any transport (%s)', (path) => {
    const send = vi.fn();
    expect(() => consentExporter(() => true, send, path)).toThrow('path must be a canonical absolute same-origin path');
    expect(send).not.toHaveBeenCalled();
  });

  it('settles a batch without transport if the consent callback throws', () => {
    const send = vi.fn();
    exporter = consentExporter(() => { throw new Error('private-storage-error'); }, send, INTAKE_PATH);
    const callback = vi.fn();
    expect(() => exporter!.export([], callback)).not.toThrow();
    expect(callback).toHaveBeenCalledOnce();
    expect(callback).toHaveBeenCalledWith({ code: ExportResultCode.SUCCESS });
    expect(send).not.toHaveBeenCalled();
  });

});
