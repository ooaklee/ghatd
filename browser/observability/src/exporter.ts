/**
 * Consent-gated, strictly serial OTLP/JSON span exporter.
 *
 * Invariants: same-origin intake only, no credentials, no retries, no
 * redirection, bounded queue, synchronous gate closure on revoke, and every
 * export callback settled exactly once.
 */
import { assertCanonicalPath } from './paths';
import { ExportResultCode, type ExportResult } from '@opentelemetry/core';
import { JsonTraceSerializer } from '@opentelemetry/otlp-transformer';
import type { ReadableSpan, SpanExporter } from '@opentelemetry/sdk-trace';

const MAX_EXPORT_SPANS = 128;
const MAX_EXPORT_BATCHES = 8;
const TRANSPORT_DEADLINE_MS = 3000;
const MAX_BATCH_BYTES = 65536;
const MAX_BATCH_SPANS = 16;

type Batch = {
  body: string;
  count: number;
  callback(result: ExportResult): void;
  settled: boolean;
};
type ActiveBatch = { batch: Batch; abort: AbortController; timer: ReturnType<typeof setTimeout> };

export function consentExporter(allowed: () => boolean, send: typeof fetch, intakePath: string): SpanExporter & { close(): void } {
  assertCanonicalPath(intakePath);
  let closed = false;
  function hasConsent(): boolean { try { return allowed() === true; } catch { return false; } }
  let active: ActiveBatch | undefined;
  let bufferedSpans = 0;
  const pending: Batch[] = [];

  function finish(batch: Batch, code: ExportResultCode): void {
    if (batch.settled) return;
    batch.settled = true;
    if (active?.batch === batch) {
      clearTimeout(active.timer);
      active = undefined;
    }
    bufferedSpans -= batch.count;
    // The SDK can synchronously schedule its next batch from this callback.
    // Release our in-flight slot first, and settle each callback once.
    batch.callback({ code });
    pump();
  }
  function close(): void {
    closed = true;
    const discarded = pending.splice(0);
    if (active) {
      const current = active;
      current.abort.abort();
      finish(current.batch, ExportResultCode.SUCCESS);
    }
    for (const batch of discarded) finish(batch, ExportResultCode.SUCCESS);
  }
  function pump(): void {
    if (closed || active || pending.length === 0) return;
    if (!hasConsent()) { close(); return; }
    const batch = pending.shift()!;
    const abort = new AbortController();
    const timer = setTimeout(() => {
      abort.abort();
      finish(batch, ExportResultCode.FAILED);
    }, TRANSPORT_DEADLINE_MS);
    active = { batch, abort, timer };
    // No retries, redirects, credentials, keepalive or trace headers on intake.
    Promise.resolve().then(() => {
      if (closed || !hasConsent()) { close(); return undefined; }
      return send(intakePath, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: batch.body, credentials: 'omit', mode: 'same-origin',
        redirect: 'error', keepalive: false, signal: abort.signal,
      });
    }).then((response) => {
      finish(batch, !response || response.status === 202 || closed ? ExportResultCode.SUCCESS : ExportResultCode.FAILED);
    }, () => finish(batch, closed ? ExportResultCode.SUCCESS : ExportResultCode.FAILED));
  }
  return {
    close,
    async shutdown() { close(); },
    export(spans: ReadableSpan[], callback) {
      if (closed || !hasConsent() || spans.length === 0) { callback({ code: ExportResultCode.SUCCESS }); return; }
      // forceFlush may submit concurrent SDK batches. Bound the exporter queue
      // separately from the SDK's 128-span queue, including the active batch.
      if (spans.length > MAX_BATCH_SPANS || bufferedSpans + spans.length > MAX_EXPORT_SPANS
        || pending.length + (active ? 1 : 0) >= MAX_EXPORT_BATCHES) {
        callback({ code: ExportResultCode.FAILED }); return;
      }
      let bytes: Uint8Array | undefined;
      try { bytes = JsonTraceSerializer.serializeRequest(spans); } catch { /* Fail invalid batches without exposing values. */ }
      if (!bytes || bytes.byteLength > MAX_BATCH_BYTES) { callback({ code: ExportResultCode.FAILED }); return; }
      const batch: Batch = { body: new TextDecoder().decode(bytes), count: spans.length, callback, settled: false };
      pending.push(batch);
      bufferedSpans += batch.count;
      pump();
    },
  };
}
