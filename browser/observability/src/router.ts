/**
 * Vue Router navigation outcome capture.
 *
 * Must not import the OpenTelemetry SDK. The telemetry handle is injected, so
 * multiple instances cannot cross-contaminate.
 */
import { isNavigationFailure, NavigationFailureType, type Router } from 'vue-router';
import type { BrowserTelemetry } from './types';

export function observeRouterWith(router: Router, getTelemetry: () => BrowserTelemetry | undefined): void {
  router.beforeEach((to) => { getTelemetry()?.beginNavigation(to, to.name, Boolean(to.redirectedFrom)); });
  router.afterEach((to, _from, failure) => {
    const telemetry = getTelemetry();
    if (!telemetry) return;
    if (failure) {
      telemetry.completeNavigation(to, isNavigationFailure(failure, NavigationFailureType.cancelled | NavigationFailureType.aborted | NavigationFailureType.duplicated) ? 'cancelled' : 'error');
      return;
    }
    telemetry.completeNavigation(to, 'complete');
  });
  router.onError((error, to) => {
    getTelemetry()?.recordError(error, 'navigation');
    getTelemetry()?.completeNavigation(to, 'error');
  });
}
