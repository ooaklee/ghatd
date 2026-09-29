/** Shared synthetic group configuration: a fixture, not a host's vocabulary. */
import type { GroupConfig } from '../src/groups';

export const INTAKE_PATH = '/api/v1/telemetry/browser/traces';

export const GROUPS: GroupConfig = {
  routes: Object.freeze({
    landing: 'home', app: 'home',
    auth: 'auth', 'auth-login': 'auth',
    settings: 'settings',
    billing: 'billing',
    portal: 'portal',
  }),
  apis: [
    ['/api/v1/ums', 'accounts'],
    ['/api/v1/bms', 'billing'],
    ['/api/v1/telemetry', 'telemetry'],
  ],
  telemetryPaths: ['/api/v1/telemetry'],
};
