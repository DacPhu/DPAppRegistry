import http from 'http';
import { Client } from '../src/client';
import {
  CheckError,
  EndpointError,
  ErrInvalidBaseURL,
  ErrMissingAppName,
  ErrMissingBaseURL,
  ErrMissingOwner,
  ErrMissingVersion,
} from '../src/errors';
import type { CheckOptions } from '../src/types';
import { createTestServer, writeJSON } from './helpers';

function defaultOptions(): CheckOptions {
  return {
    owner: 'admin',
    appName: 'test',
    version: '0.0.0.5',
    channel: 'nightly',
    platform: 'darwin',
    arch: 'arm64',
  };
}

describe('Client.checkForUpdates', () => {
  it('sends request to base API with correct params and headers', async () => {
    const server = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      expect(url.pathname).toBe('/checkVersion');
      expect(url.searchParams.get('owner')).toBe('admin');
      expect(url.searchParams.get('app_name')).toBe('test');
      expect(url.searchParams.get('version')).toBe('0.0.0.5');
      expect(url.searchParams.get('channel')).toBe('nightly');
      expect(url.searchParams.get('platform')).toBe('darwin');
      expect(url.searchParams.get('arch')).toBe('arm64');
      expect(req.headers['x-device-id']).toBe('device-1');
      expect(req.headers['user-agent']).toBeTruthy();
      writeJSON(res, { update_available: true, update_url: 'https://downloads.example/app' });
    });

    try {
      const client = new Client({ baseURL: server.url });
      const resp = await client.checkForUpdates({
        owner: 'admin',
        appName: 'test',
        version: '0.0.0.5',
        channel: 'nightly',
        platform: 'darwin',
        arch: 'arm64',
        deviceId: 'device-1',
      });
      expect(resp.updateAvailable).toBe(true);
      expect(resp.updateUrl).toBe('https://downloads.example/app');
      expect(resp.source).toBe('api');
    } finally {
      await server.close();
    }
  });

  it('uses edge static response and skips API when edge succeeds', async () => {
    let apiCalled = false;
    const apiServer = await createTestServer((_, res) => {
      apiCalled = true;
      res.writeHead(500);
      res.end();
    });

    const edgeServer = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      expect(url.pathname).toBe('/responses/admin/test/nightly/darwin/arm64/manual/0.0.0.5.json');
      writeJSON(res, { update_available: false });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const resp = await client.checkForUpdates(defaultOptions());
      expect(resp.source).toBe('edge');
      expect(apiCalled).toBe(false);
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it('normalizes dashes to dots in the edge version path segment', async () => {
    const apiServer = await createTestServer((_, res) => {
      res.writeHead(500);
      res.end();
    });

    const edgeServer = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      expect(url.pathname).toBe('/responses/admin/test/nightly/darwin/arm64/manual/2.0.0.4.json');
      writeJSON(res, { update_available: false });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const resp = await client.checkForUpdates({ ...defaultOptions(), version: '2.0.0-4' });
      expect(resp.source).toBe('edge');
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it.each([
    ['no arch', { arch: undefined }, '/responses/admin/test/nightly/darwin/manual/0.0.0.5.json'],
    ['no platform drops the updater segment', { platform: undefined }, '/responses/admin/test/nightly/arm64/0.0.0.5.json'],
    ['no platform and arch', { platform: undefined, arch: undefined }, '/responses/admin/test/nightly/0.0.0.5.json'],
  ])('matches the server object key with %s', async (_, overrides: Partial<CheckOptions>, wantPath) => {
    const apiServer = await createTestServer((_, res) => {
      res.writeHead(500);
      res.end();
    });

    let edgePath = '';
    const edgeServer = await createTestServer((req, res) => {
      edgePath = new URL(req.url!, `http://${req.headers.host}`).pathname;
      writeJSON(res, { update_available: false });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const resp = await client.checkForUpdates({ ...defaultOptions(), ...overrides });
      expect(edgePath).toBe(wantPath);
      expect(resp.source).toBe('edge');
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it('sends telemetry beacon to base API after edge success', async () => {
    let apiCalled = false;
    let telemetryCalled = false;

    const apiServer = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      if (url.pathname === '/telemetry/beacon') {
        telemetryCalled = true;
        expect(url.searchParams.get('app_name')).toBe('test');
        expect(url.searchParams.get('version')).toBe('0.0.0.5');
        expect(url.searchParams.get('channel')).toBe('nightly');
        expect(url.searchParams.get('platform')).toBe('darwin');
        expect(url.searchParams.get('arch')).toBe('arm64');
        expect(url.searchParams.get('owner')).toBe('admin');
        expect(url.searchParams.get('is_latest')).toBe('false');
        expect(req.headers['x-device-id']).toBe('device-1');
        res.writeHead(200);
        res.end();
      } else if (url.pathname === '/checkVersion') {
        apiCalled = true;
        res.writeHead(500);
        res.end();
      } else {
        res.writeHead(404);
        res.end();
      }
    });

    const edgeServer = await createTestServer((_, res) => {
      writeJSON(res, { update_available: true });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const resp = await client.checkForUpdates({
        owner: 'admin',
        appName: 'test',
        version: '0.0.0.5',
        channel: 'nightly',
        platform: 'darwin',
        arch: 'arm64',
        deviceId: 'device-1',
      });
      expect(resp.source).toBe('edge');
      expect(telemetryCalled).toBe(true);
      expect(apiCalled).toBe(false);
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it.each<{ updateAvailable: boolean; wantIsLatest: string }>([
    { updateAvailable: true, wantIsLatest: 'false' },
    { updateAvailable: false, wantIsLatest: 'true' },
  ])(
    'telemetry is_latest=$wantIsLatest when updateAvailable=$updateAvailable',
    async ({ updateAvailable, wantIsLatest }) => {
      let telemetryIsLatest = '';

      const apiServer = await createTestServer((req, res) => {
        const url = new URL(req.url!, `http://${req.headers.host}`);
        if (url.pathname === '/telemetry/beacon') {
          telemetryIsLatest = url.searchParams.get('is_latest') ?? '';
          res.writeHead(200);
          res.end();
        } else {
          res.writeHead(404);
          res.end();
        }
      });

      const edgeServer = await createTestServer((_, res) => {
        writeJSON(res, { update_available: updateAvailable });
      });

      try {
        const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
        await client.checkForUpdates({
          owner: 'admin',
          appName: 'test',
          version: '0.0.0.5',
          channel: 'nightly',
          platform: 'darwin',
          arch: 'arm64',
          deviceId: 'device-1',
        });
        expect(telemetryIsLatest).toBe(wantIsLatest);
      } finally {
        await Promise.all([apiServer.close(), edgeServer.close()]);
      }
    },
  );

  it('does not send telemetry beacon when deviceId is absent', async () => {
    let telemetryCalled = false;

    const apiServer = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      if (url.pathname === '/telemetry/beacon') {
        telemetryCalled = true;
      }
      res.writeHead(404);
      res.end();
    });

    const edgeServer = await createTestServer((_, res) => {
      writeJSON(res, { update_available: false });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const resp = await client.checkForUpdates(defaultOptions());
      expect(resp.source).toBe('edge');
      expect(telemetryCalled).toBe(false);
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it('ignores telemetry failure and returns edge response', async () => {
    const apiServer = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      if (url.pathname === '/telemetry/beacon') {
        res.writeHead(500);
        res.end('telemetry failed');
      } else {
        res.writeHead(404);
        res.end();
      }
    });

    const edgeServer = await createTestServer((_, res) => {
      writeJSON(res, { update_available: true, update_url: 'https://downloads.example/app' });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const resp = await client.checkForUpdates({
        owner: 'admin',
        appName: 'test',
        version: '0.0.0.5',
        channel: 'nightly',
        platform: 'darwin',
        arch: 'arm64',
        deviceId: 'device-1',
      });
      expect(resp.source).toBe('edge');
      expect(resp.updateUrl).toBe('https://downloads.example/app');
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it('sends downloadToken as X-Download-Token and skips the edge', async () => {
    let edgeCalled = false;
    const edgeServer = await createTestServer((_, res) => {
      edgeCalled = true;
      writeJSON(res, { update_available: false });
    });

    const apiServer = await createTestServer((req, res) => {
      expect(req.headers['x-download-token']).toBe('fnd_token');
      writeJSON(res, { update_available: true, update_url: 'https://downloads.example/app' });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const resp = await client.checkForUpdates({ ...defaultOptions(), downloadToken: 'fnd_token' });
      expect(resp.source).toBe('api');
      expect(resp.updateAvailable).toBe(true);
      expect(edgeCalled).toBe(false);
    } finally {
      await Promise.all([edgeServer.close(), apiServer.close()]);
    }
  });

  it('omits X-Download-Token when downloadToken is absent', async () => {
    const server = await createTestServer((req, res) => {
      expect(req.headers['x-download-token']).toBeUndefined();
      writeJSON(res, { update_available: false });
    });

    try {
      const client = new Client({ baseURL: server.url });
      await client.checkForUpdates(defaultOptions());
    } finally {
      await server.close();
    }
  });

  it('falls back from edge 404 to API', async () => {
    const edgeServer = await createTestServer((_, res) => {
      res.writeHead(404);
      res.end();
    });

    const apiServer = await createTestServer((_, res) => {
      writeJSON(res, { update_available: true, update_url: 'https://downloads.example/app' });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const resp = await client.checkForUpdates(defaultOptions());
      expect(resp.source).toBe('api');
    } finally {
      await Promise.all([edgeServer.close(), apiServer.close()]);
    }
  });

  it('falls back from invalid edge JSON to API', async () => {
    const edgeServer = await createTestServer((_, res) => {
      res.setHeader('Content-Type', 'application/json');
      res.end('{');
    });

    const apiServer = await createTestServer((_, res) => {
      writeJSON(res, { update_url: 'https://downloads.example/app' });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const resp = await client.checkForUpdates(defaultOptions());
      expect(resp.source).toBe('api');
    } finally {
      await Promise.all([edgeServer.close(), apiServer.close()]);
    }
  });

  it('passes channel, platform, and arch to API without normalization', async () => {
    const server = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      expect(url.searchParams.get('channel')).toBe('stable');
      expect(url.searchParams.get('platform')).toBe('macos');
      expect(url.searchParams.get('arch')).toBe('apple-silicon');
      writeJSON(res, {});
    });

    try {
      const client = new Client({ baseURL: server.url });
      await client.checkForUpdates({
        owner: 'admin',
        appName: 'test',
        version: '0.0.0.5',
        channel: 'stable',
        platform: 'macos',
        arch: 'apple-silicon',
      });
    } finally {
      await server.close();
    }
  });

  it('always uses the manual segment in the edge path', async () => {
    const apiServer = await createTestServer((_, res) => {
      res.writeHead(500);
      res.end();
    });

    const edgeServer = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      expect(url.pathname).toBe('/responses/admin/test/nightly/darwin/arm64/manual/0.0.0.5.json');
      writeJSON(res, { update_available: false });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const resp = await client.checkForUpdates(defaultOptions());
      expect(resp.source).toBe('edge');
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it('always sends updater=manual query param', async () => {
    const server = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      expect(url.searchParams.get('updater')).toBe('manual');
      writeJSON(res, { update_available: false });
    });

    try {
      const client = new Client({ baseURL: server.url });
      await client.checkForUpdates(defaultOptions());
    } finally {
      await server.close();
    }
  });

  it('decodes dynamic update_url_<package> fields into packageUrls', async () => {
    const server = await createTestServer((_, res) => {
      res.setHeader('Content-Type', 'application/json');
      res.end(
        JSON.stringify({
          update_available: true,
          update_url_deb: 'https://downloads.example/app.deb',
          update_url_rpm: 'https://downloads.example/app.rpm',
          changelog: '### Changelog\n\n- Added feature X',
          critical: true,
          is_intermediate_required: true,
          possible_rollback: true,
        }),
      );
    });

    try {
      const client = new Client({ baseURL: server.url });
      const resp = await client.checkForUpdates(defaultOptions());
      expect(resp.updateAvailable).toBe(true);
      expect(resp.changelog).toBeTruthy();
      expect(resp.critical).toBe(true);
      expect(resp.isIntermediateRequired).toBe(true);
      expect(resp.possibleRollback).toBe(true);
      expect(resp.updateUrl).toBe('');
      expect(resp.packageUrls).toHaveLength(2);
      expect(resp.packageUrls[0]).toEqual({ package: 'deb', url: 'https://downloads.example/app.deb' });
      expect(resp.packageUrls[1]).toEqual({ package: 'rpm', url: 'https://downloads.example/app.rpm' });
    } finally {
      await server.close();
    }
  });

  it('decodes binary update_url field with no packageUrls', async () => {
    const server = await createTestServer((_, res) => {
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ update_available: true, update_url: 'https://downloads.example/app' }));
    });

    try {
      const client = new Client({ baseURL: server.url });
      const resp = await client.checkForUpdates(defaultOptions());
      expect(resp.updateUrl).toBe('https://downloads.example/app');
      expect(resp.packageUrls).toHaveLength(0);
    } finally {
      await server.close();
    }
  });

  describe('staged rollout', () => {
    const SEED = 'badadc23b08e3943';

    function rolloutServer(): Promise<import('./helpers').TestServer> {
      return createTestServer((_, res) => {
        writeJSON(res, {
          update_available: true,
          update_url: 'https://downloads.example/app',
          rollout: { percent: 20, seed: SEED },
        });
      });
    }

    it('keeps the update when the device is inside the rollout bucket', async () => {
      const server = await rolloutServer();
      try {
        const client = new Client({ baseURL: server.url });
        const resp = await client.checkForUpdates({ ...defaultOptions(), deviceId: 'device-1' });
        expect(resp.updateAvailable).toBe(true);
        expect(resp.updateUrl).toBe('https://downloads.example/app');
        expect(resp.rollout).toEqual({ percent: 20, seed: SEED, bucket: 18, eligible: true });
      } finally {
        await server.close();
      }
    });

    it('gates the update and blanks the URLs when outside the rollout bucket', async () => {
      const server = await createTestServer((_, res) => {
        writeJSON(res, {
          update_available: true,
          update_url: 'https://downloads.example/app',
          update_url_dmg: 'https://downloads.example/app.dmg',
          rollout: { percent: 20, seed: SEED },
        });
      });
      try {
        const client = new Client({ baseURL: server.url });
        const resp = await client.checkForUpdates({ ...defaultOptions(), deviceId: 'device-2' });
        expect(resp.updateAvailable).toBe(false);
        expect(resp.updateUrl).toBe('');
        expect(resp.packageUrls).toEqual([]);
        expect(resp.rollout).toEqual({ percent: 20, seed: SEED, bucket: 22, eligible: false });
      } finally {
        await server.close();
      }
    });

    it('gates the update and blanks the URLs when no deviceId is supplied', async () => {
      const server = await rolloutServer();
      try {
        const client = new Client({ baseURL: server.url });
        const resp = await client.checkForUpdates(defaultOptions());
        expect(resp.updateAvailable).toBe(false);
        expect(resp.updateUrl).toBe('');
        expect(resp.packageUrls).toEqual([]);
        expect(resp.rollout).toEqual({ percent: 20, seed: SEED, bucket: null, eligible: false });
      } finally {
        await server.close();
      }
    });

    it('leaves the response untouched when no rollout is present', async () => {
      const server = await createTestServer((_, res) => {
        writeJSON(res, { update_available: true, update_url: 'https://downloads.example/app' });
      });
      try {
        const client = new Client({ baseURL: server.url });
        const resp = await client.checkForUpdates({ ...defaultOptions(), deviceId: 'device-2' });
        expect(resp.updateAvailable).toBe(true);
        expect(resp.rollout).toBeUndefined();
      } finally {
        await server.close();
      }
    });
  });

  describe('input validation', () => {
    it.each<{ name: string; baseURL: string; opts: CheckOptions; want: Error }>([
      {
        name: 'missing base URL',
        baseURL: '',
        opts: defaultOptions(),
        want: ErrMissingBaseURL,
      },
      {
        name: 'invalid base URL (no hostname)',
        baseURL: 'file:///path/to/api',
        opts: defaultOptions(),
        want: ErrInvalidBaseURL,
      },
      {
        name: 'missing owner',
        baseURL: 'https://api.example',
        opts: { owner: '', appName: 'test', version: '0.0.0.5' },
        want: ErrMissingOwner,
      },
      {
        name: 'missing app name',
        baseURL: 'https://api.example',
        opts: { owner: 'admin', appName: '', version: '0.0.0.5' },
        want: ErrMissingAppName,
      },
      {
        name: 'missing version',
        baseURL: 'https://api.example',
        opts: { owner: 'admin', appName: 'test', version: '' },
        want: ErrMissingVersion,
      },
    ])('rejects with $name error', async ({ baseURL, opts, want }) => {
      const client = new Client({ baseURL });
      await expect(client.checkForUpdates(opts)).rejects.toBe(want);
    });
  });

  it('supports custom fetch implementation', async () => {
    const server = await createTestServer((_, res) => {
      writeJSON(res, { update_available: true });
    });

    let customFetchCalled = false;
    const customFetch: typeof globalThis.fetch = (input, init) => {
      customFetchCalled = true;
      return globalThis.fetch(input, init);
    };

    try {
      const client = new Client({ baseURL: server.url, fetch: customFetch });
      await client.checkForUpdates(defaultOptions());
      expect(customFetchCalled).toBe(true);
    } finally {
      await server.close();
    }
  });

  it('accepts custom HTTP server via fetch config', async () => {
    const server = await createTestServer((req: http.IncomingMessage, res: http.ServerResponse) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      expect(url.pathname).toBe('/checkVersion');
      writeJSON(res, { update_available: false });
    });

    try {
      const client = new Client({ baseURL: server.url });
      const resp = await client.checkForUpdates(defaultOptions());
      expect(resp.updateAvailable).toBe(false);
    } finally {
      await server.close();
    }
  });

  it('rejects with ErrInvalidBaseURL for a non-absolute base URL', async () => {
    const client = new Client({ baseURL: 'not-a-url' });
    await expect(client.checkForUpdates(defaultOptions())).rejects.toBe(ErrInvalidBaseURL);
  });

  it('throws CheckError with both edgeError and apiError when all endpoints fail', async () => {
    const edgeServer = await createTestServer((_, res) => {
      res.writeHead(404);
      res.end();
    });

    const apiServer = await createTestServer((_, res) => {
      res.writeHead(500);
      res.end();
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const err = await client.checkForUpdates(defaultOptions()).catch((e) => e);
      expect(err).toBeInstanceOf(CheckError);
      expect((err as CheckError).edgeError).toBeInstanceOf(EndpointError);
      expect((err as CheckError).apiError).toBeInstanceOf(EndpointError);
    } finally {
      await Promise.all([edgeServer.close(), apiServer.close()]);
    }
  });

  it('wraps a fetch network error in EndpointError inside CheckError', async () => {
    const networkError = new TypeError('fetch failed');
    const client = new Client({
      baseURL: 'http://api.example.com',
      fetch: () => Promise.reject(networkError),
    });

    const err = await client.checkForUpdates(defaultOptions()).catch((e) => e);
    expect(err).toBeInstanceOf(CheckError);
    const apiError = (err as CheckError).apiError as EndpointError;
    expect(apiError).toBeInstanceOf(EndpointError);
    expect(apiError.cause).toBe(networkError);
  });

  it('builds correct API URL when baseURL has a path prefix', async () => {
    const server = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      expect(url.pathname).toBe('/v1/checkVersion');
      writeJSON(res, {});
    });

    try {
      const client = new Client({ baseURL: `${server.url}/v1` });
      await client.checkForUpdates(defaultOptions());
    } finally {
      await server.close();
    }
  });

  it('does not fall back to API when AbortSignal is already aborted', async () => {
    const edgeServer = await createTestServer((_, res) => {
      res.writeHead(404);
      res.end();
    });

    let apiCalled = false;
    const apiServer = await createTestServer((_, res) => {
      apiCalled = true;
      writeJSON(res, { update_available: false });
    });

    const controller = new AbortController();
    controller.abort(new Error('cancelled'));

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const err = await client.checkForUpdates(defaultOptions(), controller.signal).catch((e) => e);
      expect(err).toBeInstanceOf(CheckError);
      expect(apiCalled).toBe(false);
    } finally {
      await Promise.all([edgeServer.close(), apiServer.close()]);
    }
  });

  it('succeeds with a live (non-aborted) AbortSignal', async () => {
    const server = await createTestServer((_, res) => {
      writeJSON(res, { update_available: false });
    });

    const controller = new AbortController();

    try {
      const client = new Client({ baseURL: server.url });
      const resp = await client.checkForUpdates(defaultOptions(), controller.signal);
      expect(resp.updateAvailable).toBe(false);
    } finally {
      await server.close();
    }
  });
});
