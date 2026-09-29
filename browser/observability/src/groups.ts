/**
 * Generic finite route/API group vocabulary.
 *
 * This module must not import the OpenTelemetry SDK: facade, axios and router
 * integrations consume it directly and the SDK stays a controller-only cost.
 */

import { assertCanonicalPath } from './paths';

export interface GroupConfig {
  /** Static route-name to group map. Unknown names map to "other". */
  readonly routes: Readonly<Record<string, string>>;
  /** API prefix to group tuples; the first matching boundary wins. */
  readonly apis: ReadonlyArray<readonly [string, string]>;
  /** Absolute intake path(s) that must never produce request telemetry. */
  readonly telemetryPaths?: ReadonlyArray<string>;
  /** Custom intake path, excluded in addition to the default telemetry subtree. */
  readonly intakePath?: string;
}

export interface RouteGroups {
  routeGroup(name: unknown): string;
  apiGroup(uri: string, origin: string): string | undefined;
  /** Normalizes an already-resolved API group from a runtime caller. */
  normalizeAPIGroup(group: unknown): string;
}

const METHOD_VOCABULARY = ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'] as const;

const GROUP = /^[a-z][a-z0-9_-]{0,31}$/;
const MAX_GROUPS = 32;

function validateVocabulary(values: readonly unknown[]): Set<string> {
  const vocabulary = new Set(['other']);
  for (const value of values) {
    if (typeof value !== 'string' || !GROUP.test(value)) throw new TypeError('group must use the bounded group format');
    vocabulary.add(value);
  }
  if (vocabulary.size > MAX_GROUPS) throw new TypeError('group vocabulary must contain at most 32 values including other');
  return vocabulary;
}

const boundary = (path: string, prefix: string): boolean => prefix === '/' || path === prefix || path.startsWith(`${prefix}/`);

/**
 * Builds a finite, host-configured group resolver. The configuration is fixed
 * at construction time so no runtime input can grow the label vocabulary.
 */
export function createGroups(config: GroupConfig): RouteGroups {
  if (!config || typeof config.routes !== 'object' || config.routes === null || Array.isArray(config.routes)
    || !Array.isArray(config.apis)) throw new TypeError('groups require a route map and API tuple list');
  const routes: Readonly<Record<string, string>> = Object.freeze({ ...config.routes });
  validateVocabulary(Object.values(routes));
  if (Object.keys(routes).some((name) => name.length === 0)) throw new TypeError('route names must be non-empty');
  const apis = Object.freeze(config.apis.map((entry) => {
    if (!Array.isArray(entry) || entry.length !== 2) throw new TypeError('API entries must be prefix and group tuples');
    const [prefix, group] = entry;
    assertCanonicalPath(prefix);
    return Object.freeze([prefix.replace(/\/$/, '') || '/', group] as const);
  }));
  const apiVocabulary = validateVocabulary(apis.map((entry) => entry[1]));
  if (config.telemetryPaths !== undefined && !Array.isArray(config.telemetryPaths)) throw new TypeError('telemetry paths must be a list');
  const telemetryPaths = Object.freeze([
    '/api/v1/telemetry', ...(config.telemetryPaths ?? []), ...(config.intakePath === undefined ? [] : [config.intakePath]),
  ].map((path) => {
    assertCanonicalPath(path);
    return path.replace(/\/$/, '') || '/';
  }));

  function routeGroup(name: unknown): string {
    return typeof name === 'string' && Object.hasOwn(routes, name) ? routes[name] : 'other';
  }

  function apiGroup(uri: string, origin: string): string | undefined {
    try {
      // Reject ambiguous path encodings before URL normalization. Queries stay local.
      const pathPart = uri.split(/[?#]/, 1)[0];
      if (/[\s\p{Cc}]/u.test(pathPart) || pathPart.includes('\\') || pathPart.includes('%') || /(?:^|\/)\.{1,2}(?:\/|$)/.test(pathPart)) return;
      const url = new URL(uri, `${origin}/`);
      if (url.origin !== origin || url.username || url.password || !url.pathname.startsWith('/api/')
        || url.pathname.includes('//') || telemetryPaths.some((path) => boundary(url.pathname, path))) return;
      return apis.find(([prefix]) => boundary(url.pathname, prefix))?.[1] ?? 'other';
    } catch { return; }
  }

  return Object.freeze({ routeGroup, apiGroup, normalizeAPIGroup: (group: unknown) =>
    typeof group === 'string' && apiVocabulary.has(group) ? group : 'other' });
}

export function requestMethod(method: string): string {
  const upper = typeof method === 'string' ? method.toUpperCase() : 'OTHER';
  return (METHOD_VOCABULARY as readonly string[]).includes(upper) ? upper : 'OTHER';
}

export function errorType(error: unknown): string {
  // Never read user-controlled .name/.message/.stack or stringify the thrown value.
  try {
    if (error instanceof TypeError) return 'type-error';
    if (error instanceof ReferenceError) return 'reference-error';
    if (error instanceof RangeError) return 'range-error';
    if (error instanceof SyntaxError) return 'syntax-error';
    if (error instanceof URIError) return 'uri-error';
    if (error instanceof EvalError) return 'eval-error';
    if (error instanceof AggregateError) return 'aggregate-error';
    if (error instanceof Error) return 'error';
  } catch { /* A hostile proxy is still just an unknown error. */ }
  return 'unknown';
}
