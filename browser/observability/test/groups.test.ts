import { describe, expect, it } from 'vitest';
import { createGroups, type GroupConfig } from '../src/groups';

const origin = 'https://example.invalid';

describe('bounded immutable group configuration', () => {
  it.each(['Private', 'with.dot', 'with space', 'raw/id', '', 'a'.repeat(33)])('rejects invalid group format %s without including it in errors', (group) => {
    expect(() => createGroups({ routes: { page: group }, apis: [] })).toThrow('group must use the bounded group format');
    expect(() => createGroups({ routes: {}, apis: [['/api/v1/items', group]] })).toThrow('group must use the bounded group format');
  });

  it('counts the fallback against the 32-group bound independently for routes and APIs', () => {
    const values = Array.from({ length: 31 }, (_, index) => `group_${index}`);
    const routes = Object.fromEntries(values.map((value) => [value, value]));
    const apis = values.map((value) => [`/api/${value}`, value] as const);
    expect(() => createGroups({ routes, apis })).not.toThrow();
    expect(() => createGroups({ routes: { ...routes, one_more: 'one_more' }, apis: [] })).toThrow('at most 32');
    expect(() => createGroups({ routes: {}, apis: [...apis, ['/api/one_more', 'one_more']] })).toThrow('at most 32');
    expect(() => createGroups({ routes: { ...routes, other: 'other' }, apis: [...apis, ['/api/other', 'other']] })).not.toThrow();
  });

  it.each([null, undefined, 4, {}, ['/api/x'], ['/api/x', 'x', 'extra']])('validates tuple structure before reading entries (%s)', (entry) => {
    expect(() => createGroups({ routes: {}, apis: [entry] } as unknown as GroupConfig)).toThrow('API entries must be prefix and group tuples');
  });

  it('copies configuration, preserving vocabulary and excluded paths after caller mutation', () => {
    const routes = { page: 'public' };
    const apis: [string, string][] = [['/api/v1/items', 'items']];
    const telemetryPaths = ['/api/v1/private-intake'];
    const groups = createGroups({ routes, apis, telemetryPaths });
    routes.page = 'changed';
    apis[0][0] = '/api/v1/changed';
    apis[0][1] = 'changed';
    apis.push(['/api/v1/new', 'new']);
    telemetryPaths[0] = '/api/v1/changed-intake';
    expect(groups.routeGroup('page')).toBe('public');
    expect(groups.apiGroup('/api/v1/items/123', origin)).toBe('items');
    expect(groups.apiGroup('/api/v1/new', origin)).toBe('other');
    expect(groups.apiGroup('/api/v1/private-intake', origin)).toBeUndefined();
    expect(groups.normalizeAPIGroup('changed')).toBe('other');
  });

  it('always excludes default telemetry and custom intake subtrees', () => {
    const groups = createGroups({ routes: {}, apis: [['/api', 'api']], intakePath: '/api/custom-intake' });
    for (const uri of ['/api/v1/telemetry', '/api/v1/telemetry/browser/traces', '/api/custom-intake', '/api/custom-intake/child']) {
      expect(groups.apiGroup(uri, origin)).toBeUndefined();
    }
    expect(groups.apiGroup('/api/custom-intake-alias', origin)).toBe('api');
  });

  it.each(['//example.invalid/intake', '/api//intake', '/api/%74elemetry', '/api/../intake', '/api/./intake', '/api/intake?key=private', '/api/intake#private', '/api/with space', '/api/with\\slash', '/api/with\nnewline'])('rejects ambiguous configured paths (%s)', (path) => {
    expect(() => createGroups({ routes: {}, apis: [[path, 'api']] })).toThrow('path must be a canonical absolute same-origin path');
    expect(() => createGroups({ routes: {}, apis: [], intakePath: path })).toThrow('path must be a canonical absolute same-origin path');
    expect(() => createGroups({ routes: {}, apis: [], telemetryPaths: [path] })).toThrow('path must be a canonical absolute same-origin path');
  });
});
