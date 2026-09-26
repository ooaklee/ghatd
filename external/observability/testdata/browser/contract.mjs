// Runs only from the opt-in Go contract. Use real SDK spans and the official
// serializer so changes to OTLP JSON IDs, numeric values or timing are visible.
import {
  ROOT_CONTEXT,
  SpanKind,
  SpanStatusCode,
  TraceFlags,
  createTraceState,
  trace,
} from '@opentelemetry/api';
import { W3CTraceContextPropagator } from '@opentelemetry/core';
import { JsonTraceSerializer } from '@opentelemetry/otlp-transformer';
import { resourceFromAttributes } from '@opentelemetry/resources';
import {
  AlwaysOnSampler,
  InMemorySpanExporter,
  SimpleSpanProcessor,
  TracerProvider,
} from '@opentelemetry/sdk-trace';

const canary = 'synthetic-private-browser-input';

async function run() {
  const chunks = [];
  let length = 0;
  for await (const chunk of process.stdin) {
    length += chunk.length;
    if (length > 4096) throw new Error('invalid fixture input');
    chunks.push(chunk);
  }
  const options = JSON.parse(Buffer.concat(chunks).toString('utf8'));
  const origin = new URL(options.origin);
  if (origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' ||
      origin.pathname !== '/' || origin.search || origin.hash || origin.username || origin.password) {
    throw new Error('invalid fixture origin');
  }
  const hostile = options.mode === 'hostile';
  const exporter = new InMemorySpanExporter();
  const provider = new TracerProvider({
    resource: resourceFromAttributes({
      'service.name': hostile ? canary : 'untrusted-browser-name',
      'service.namespace': hostile ? canary : 'untrusted-namespace',
      'service.instance.id': hostile ? canary : 'untrusted-instance',
      'deployment.environment.name': hostile ? canary : 'untrusted-environment',
    }, { schemaUrl: hostile ? canary : undefined }),
    sampler: new AlwaysOnSampler(),
    spanProcessors: [new SimpleSpanProcessor({ exporter })],
  });
  try {
    const tracer = provider.getTracer(hostile ? canary : 'untrusted-browser-scope', '0.0.0', {
      schemaUrl: hostile ? canary : undefined,
    });
    const parentContext = hostile ? trace.setSpanContext(ROOT_CONTEXT, {
      traceId: '0102030405060708090a0b0c0d0e0f10',
      spanId: '1112131415161718',
      traceFlags: TraceFlags.SAMPLED,
      isRemote: true,
      traceState: createTraceState('vendor=' + canary),
    }) : ROOT_CONTEXT;
    const navigation = tracer.startSpan('browser.navigation', {
      kind: SpanKind.INTERNAL,
      attributes: { 'browser.route.group': 'home', 'browser.outcome': 'complete' },
    }, parentContext);
    const navigationContext = trace.setSpan(ROOT_CONTEXT, navigation);
    const request = tracer.startSpan('browser.request', {
      kind: SpanKind.CLIENT,
      attributes: {
        'browser.route.group': 'home', 'browser.api.group': 'orders',
        'http.request.method': 'POST', 'browser.outcome': 'success',
      },
    }, navigationContext);
    const headers = { 'Content-Type': 'application/json', Authorization: canary };
    new W3CTraceContextPropagator().inject(trace.setSpan(ROOT_CONTEXT, request), headers, {
      set(carrier, key, value) {
        if (key === 'traceparent') carrier[key] = value;
      },
    });
    const businessResponse = await fetch(new URL('/api/v1/orders/' + canary + '?private=' + canary, origin), {
      method: 'POST', headers, body: JSON.stringify({ private: canary }),
      redirect: 'error', credentials: 'omit', keepalive: false, signal: AbortSignal.timeout(3000),
    });
    if (businessResponse.status !== 200) throw new Error('fixture request failed');
    const business = await businessResponse.json();
    request.setAttribute('http.response.status_code', businessResponse.status);

    const document = tracer.startSpan('browser.document', {
      kind: SpanKind.INTERNAL,
      attributes: {
        'browser.route.group': 'home',
        'browser.document.ttfb_ms': 12.5,
        'browser.document.dom_content_loaded_ms': 25.25,
        'browser.document.load_ms': 35.75,
      },
    }, ROOT_CONTEXT);
    const error = tracer.startSpan('browser.error', {
      kind: SpanKind.INTERNAL,
      attributes: {
        'browser.route.group': 'home', 'browser.error.source': 'window', 'error.type': 'type-error',
      },
      links: hostile ? [{
        context: request.spanContext(), attributes: { 'untrusted.link': canary },
      }] : [],
    }, navigationContext);
    error.setStatus({ code: SpanStatusCode.ERROR });
    if (hostile) {
      for (const span of [navigation, request, document, error]) {
        span.setAttribute('url.full', origin.origin + '/private/' + canary);
        span.setAttribute('untrusted.value', canary);
        span.setStatus({ code: SpanStatusCode.ERROR, message: canary });
        span.recordException(new TypeError(canary));
      }
    }
    request.end();
    document.end();
    error.end();
    navigation.end();
    await provider.forceFlush();
    // InMemorySpanExporter.shutdown clears finished spans, so serialization
    // must happen while this fixture's provider is still alive.
    const payload = JsonTraceSerializer.serializeRequest(exporter.getFinishedSpans());
    if (!payload || payload.byteLength > 65536) throw new Error('invalid fixture payload');
    const intakeHeaders = { 'Content-Type': 'application/json', Origin: origin.origin };
    const intakeResponse = await fetch(new URL('/api/v1/telemetry/browser/traces', origin), {
      method: 'POST', headers: intakeHeaders, body: payload,
      redirect: 'error', credentials: 'omit', keepalive: false, signal: AbortSignal.timeout(3000),
    });
    const intakeBody = await intakeResponse.text();
    if (intakeResponse.status !== 202 || intakeBody !== '') throw new Error('fixture intake failed');
    process.stdout.write(JSON.stringify({
      wire: Buffer.from(payload).toString('base64'),
      traceparent: headers.traceparent,
      business,
      intakeStatus: intakeResponse.status,
      intakeHeaders: Object.keys(intakeHeaders),
    }));
  } finally {
    await provider.shutdown();
  }
}

try {
  await run();
} catch {
  // Do not expose caught exceptions, response bodies, URLs or fixture canaries
  // through a failed test subprocess. Go emits its own fixed diagnostic.
  process.stderr.write('browser contract fixture failed\n');
  process.exitCode = 1;
}
