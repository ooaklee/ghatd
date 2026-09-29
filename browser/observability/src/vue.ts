/** Optional Vue hooks. Only this subpath imports the Vue runtime. */
import { nextTick, type App } from 'vue';
import type { BrowserFrameworkIntegration } from './types';

/** Keeps the host handler chain and restores it only while this hook owns it. */
export function createVueIntegration(app: App): BrowserFrameworkIntegration {
  return {
    subscribeErrors(record) {
      const previous = app.config.errorHandler;
      // Vue already rethrows to the controller's window listener in this mode.
      if (!previous && app.config.throwUnhandledErrorInProduction) return () => {};
      const handler: NonNullable<App['config']['errorHandler']> = (error, instance, info) => {
        record(error, 'vue');
        if (previous) return previous(error, instance, info);
        throw error;
      };
      app.config.errorHandler = handler;
      return () => {
        if (app.config.errorHandler === handler) app.config.errorHandler = previous;
      };
    },
    postRender: nextTick,
  };
}
