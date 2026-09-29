import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'jsdom',
    include: ['browser/observability/test/**/*.test.ts'],
  },
});
