/**
 * Optional single-controller facade. Multiple controllers should inject their own getters.
 *
 * This module must not import the OpenTelemetry SDK; application services can
 * exist before controller startup and remain bundle-split from SDK code.
 */
import type { BrowserTelemetry } from './types';

let current: BrowserTelemetry | undefined;

export function browserTelemetry(): BrowserTelemetry | undefined { return current; }
export function setBrowserTelemetry(value: BrowserTelemetry | undefined): void { current = value; }

export type { BrowserTelemetry, ConsentSource, ExpiringConsentSource, ConsentChangeBus, ErrorSource, NavigationOutcome, RequestOutcome, RequestMeasurement } from './types';
