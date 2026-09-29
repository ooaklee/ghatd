import { afterEach, describe, expect, it, vi } from 'vitest';
import { createApp, nextTick } from 'vue';
import { createVueIntegration } from '../src/vue';

describe('Vue error integration', () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it('chains the previous handler with identical arguments and restores it on cleanup', () => {
    const recorder = vi.fn();
    const previous = vi.fn();
    const app = createApp({});
    app.config.errorHandler = previous;
    const cleanup = createVueIntegration(app).subscribeErrors(recorder);
    const error = new SyntaxError('private-component-message');
    app.config.errorHandler!(error, null, 'private-component-info');
    expect(recorder).toHaveBeenCalledWith(error, 'vue');
    expect(previous).toHaveBeenCalledWith(error, null, 'private-component-info');
    cleanup();
    expect(app.config.errorHandler).toBe(previous);
  });

  it('rethrows the identical error when no previous handler exists', () => {
    const recorder = vi.fn();
    const app = createApp({});
    const cleanup = createVueIntegration(app).subscribeErrors(recorder);
    const error = new RangeError('private');
    expect(() => app.config.errorHandler!(error, null, 'setup')).toThrow(error);
    expect(recorder).toHaveBeenCalledWith(error, 'vue');
    cleanup();
    expect(app.config.errorHandler).toBeUndefined();
  });

  it('leaves production-rethrow mode unchanged when there is no previous handler', () => {
    const app = createApp({});
    app.config.throwUnhandledErrorInProduction = true;
    const cleanup = createVueIntegration(app).subscribeErrors(vi.fn());
    expect(app.config.errorHandler).toBeUndefined();
    cleanup();
  });

  it('still chains an existing production handler and never replaces a newer handler on cleanup', () => {
    const previous = vi.fn();
    const app = createApp({});
    app.config.throwUnhandledErrorInProduction = true;
    app.config.errorHandler = previous;
    const cleanup = createVueIntegration(app).subscribeErrors(vi.fn());
    const error = new Error('private');
    app.config.errorHandler!(error, null, 'setup');
    expect(previous).toHaveBeenCalledWith(error, null, 'setup');
    const newer = vi.fn();
    app.config.errorHandler = newer;
    cleanup();
    expect(app.config.errorHandler).toBe(newer);
  });

  it('uses Vue postRender timing', async () => {
    const integration = createVueIntegration(createApp({}));
    let ticked = false;
    void integration.postRender().then(() => { ticked = true; });
    await nextTick();
    expect(ticked).toBe(true);
  });
});
