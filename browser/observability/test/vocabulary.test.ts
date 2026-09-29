import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const ROOT = join(__dirname, '..', 'src');

/** Shared infrastructure must not own storage or product consent policy. */
const FORBIDDEN = ['localStorage', 'sessionStorage', 'document.cookie', 'indexedDB'];

describe('shared-source vocabulary and isolation', () => {
  it('does not read host persistence or cookie policy', () => {
    for (const file of readdirSync(ROOT)) {
      const content = readFileSync(join(ROOT, file), 'utf8');
      for (const term of FORBIDDEN) {
        expect(content.includes(term), `${file} contains forbidden term "${term}"`).toBe(false);
      }
    }
  });

  it('keeps SDK imports out of facade, groups, axios, router and vue modules', () => {
    for (const file of ['facade.ts', 'groups.ts', 'axios.ts', 'router.ts', 'vue.ts', 'index.ts']) {
      const content = readFileSync(join(ROOT, file), 'utf8');
      expect(content, `${file} must not import the SDK`).not.toMatch(/@opentelemetry\/(sdk-trace|resources|otlp-transformer|core)/);
    }
  });

  it('imports the SDK only from the controller and exporter', () => {
    for (const file of readdirSync(ROOT)) {
      if (file === 'controller.ts' || file === 'exporter.ts') continue;
      const content = readFileSync(join(ROOT, file), 'utf8');
      expect(content.includes('@opentelemetry/sdk-trace'), `${file} must not import sdk-trace`).toBe(false);
    }
  });

  it('keeps optional framework imports out of the root and controller', () => {
    for (const file of ['index.ts', 'controller.ts']) {
      const content = readFileSync(join(ROOT, file), 'utf8');
      expect(content).not.toMatch(/(?:from|import)\s*['"](?:vue|vue-router|axios|\.\/vue|\.\/router|\.\/axios)['"]/);
    }
  });

  it('declares no install hooks in package.json', () => {
    const pkg = JSON.parse(readFileSync(join(__dirname, '..', '..', '..', 'package.json'), 'utf8'));
    const scripts = pkg.scripts ?? {};
    expect(scripts).not.toHaveProperty('prepare');
    expect(Object.keys(scripts).some((key) => /^(preinstall|postinstall|prepare)$/.test(key))).toBe(false);
  });
});
