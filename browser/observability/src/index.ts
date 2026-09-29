/**
 * Package entry: SDK-free surface only.
 *
 * The controller (and with it the OpenTelemetry SDK) must be imported lazily
 * by hosts, e.g. `await import('@ghatd/browser-observability/controller')`,
 * so facade/group/integration modules stay out of the SDK bundle.
 */
/**
 * Optional peers (axios, vue, vue-router) are intentionally NOT re-exported here;
 * hosts import `@ghatd/browser-observability/axios` or `/router` subpaths
 * directly so the entry stays importable without those peers installed.
 */
export { browserTelemetry, setBrowserTelemetry } from './facade';
export { createGroups, requestMethod, errorType, type GroupConfig, type RouteGroups } from './groups';
export type { BrowserFrameworkIntegration, BrowserTelemetry, ConsentSource, ExpiringConsentSource, ConsentChangeBus, ErrorSource, NavigationOutcome, RequestOutcome, RequestMeasurement } from './types';
