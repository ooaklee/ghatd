import { afterEach, describe, expect, it, vi } from 'vitest';
import { nextTick } from 'vue';
import { createMemoryHistory, createRouter } from 'vue-router';
import { observeRouterWith } from '../src/router';
import { createBrowserTelemetry } from '../src/controller';
import { setBrowserTelemetry, type BrowserTelemetry } from '../src/facade';
import { syntheticConsent } from './consent';
import { GROUPS, INTAKE_PATH } from './groups';

let telemetry: BrowserTelemetry | undefined;
afterEach(async () => { setBrowserTelemetry(undefined); await telemetry?.stop(); vi.useRealTimers(); vi.restoreAllMocks(); });

describe('router outcomes', () => {
  it('records redirects and thrown guards without treating either as a completed render', async () => {
    vi.useFakeTimers();
    const send = vi.fn().mockResolvedValue({ status: 202 });
    telemetry = createBrowserTelemetry({ consent: syntheticConsent(true), serviceName: 'host-web', scope: 'example.com/host', groups: GROUPS, intakePath: INTAKE_PATH, fetch: send });
    setBrowserTelemetry(telemetry);
    const router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/', name: 'landing', component: {} },
      { path: '/portal/:portalNanoId', name: 'portal', component: {} },
      { path: '/auth/login', name: 'auth-login', component: {} },
      { path: '/settings', name: 'settings', component: {} },
    ] });
    observeRouterWith(router, () => telemetry);
    const privateFailure = new TypeError('private-guard-error');
    router.beforeEach((to) => {
      if (to.name === 'portal') return { name: 'auth-login' };
      if (to.name === 'settings') throw privateFailure;
    });
    await router.push('/portal/private-portal-id?prompt=private-prompt');
    await nextTick();
    await vi.advanceTimersByTimeAsync(40);
    expect(router.currentRoute.value.name).toBe('auth-login');
    await expect(router.push('/settings')).rejects.toBe(privateFailure);
    await telemetry.forceFlush();
    const emitted = send.mock.calls.flatMap(([, init]) => JSON.parse(init.body).resourceSpans.flatMap((resource: any) => resource.scopeSpans.flatMap((scope: any) => scope.spans)));
    const navigation = emitted.filter((span: any) => span.name === 'browser.navigation').map((span: any) => Object.fromEntries(span.attributes.map((attribute: any) => [attribute.key, attribute.value.stringValue])));
    expect(navigation).toEqual([
      { 'browser.route.group': 'portal', 'browser.outcome': 'redirected' },
      { 'browser.route.group': 'auth', 'browser.outcome': 'complete' },
      { 'browser.route.group': 'settings', 'browser.outcome': 'error' },
    ]);
    expect(emitted.filter((span: any) => span.name === 'browser.error')).toHaveLength(1);
    expect(JSON.stringify(send.mock.calls)).not.toContain('private-');
  });

  it('uses only the injected getter so a missing telemetry stays silent', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [
      { path: '/', name: 'landing', component: {} },
    ] });
    observeRouterWith(router, () => undefined);
    await router.push('/');
    expect(router.currentRoute.value.name).toBe('landing');
  });
});
