import { Client } from '../src/client';
import { ErrInvalidBaseURL, ErrMissingArch, ErrMissingPlatform, UnsupportedUpdaterError } from '../src/errors';
import { NATIVE_UPDATERS } from '../src/feed';
import type { NativeFeedOptions } from '../src/types';
import { createTestServer, writeJSON } from './helpers';

function feedOptions(overrides: Partial<NativeFeedOptions> = {}): NativeFeedOptions {
  return {
    owner: 'admin',
    appName: 'demo',
    version: '0.0.1',
    channel: 'nightly',
    platform: 'darwin',
    arch: 'arm64',
    updater: 'squirrel_darwin',
    ...overrides,
  };
}

describe('Client.buildNativeFeedURL', () => {
  const client = new Client({ baseURL: 'https://api.example.com' });

  it('builds a /checkVersion feed URL for squirrel_darwin', () => {
    const url = new URL(client.buildNativeFeedURL(feedOptions()));
    expect(url.pathname).toBe('/checkVersion');
    expect(url.searchParams.get('app_name')).toBe('demo');
    expect(url.searchParams.get('version')).toBe('0.0.1');
    expect(url.searchParams.get('channel')).toBe('nightly');
    expect(url.searchParams.get('platform')).toBe('darwin');
    expect(url.searchParams.get('arch')).toBe('arm64');
    expect(url.searchParams.get('owner')).toBe('admin');
    expect(url.searchParams.get('updater')).toBe('squirrel_darwin');
  });

  it('builds an /update path feed URL for squirrel_windows', () => {
    const url = client.buildNativeFeedURL(
      feedOptions({ updater: 'squirrel_windows', platform: 'win32', arch: 'x64' }),
    );
    expect(url).toBe('https://api.example.com/update/admin/demo/nightly/win32/x64/0.0.1');
  });

  it('preserves a base URL path prefix', () => {
    const prefixed = new Client({ baseURL: 'https://api.example.com/api/v1' });
    expect(prefixed.buildNativeFeedURL(feedOptions({ updater: 'squirrel_windows' }))).toBe(
      'https://api.example.com/api/v1/update/admin/demo/nightly/darwin/arm64/0.0.1',
    );
    const darwin = new URL(prefixed.buildNativeFeedURL(feedOptions()));
    expect(darwin.pathname).toBe('/api/v1/checkVersion');
  });

  it('throws UnsupportedUpdaterError for an unknown updater', () => {
    expect(() =>
      client.buildNativeFeedURL(feedOptions({ updater: 'velopack' as never })),
    ).toThrow(UnsupportedUpdaterError);
  });

  it('validates required coordinates', () => {
    expect(() => client.buildNativeFeedURL(feedOptions({ platform: '' }))).toThrow(ErrMissingPlatform);
    expect(() => client.buildNativeFeedURL(feedOptions({ arch: '' }))).toThrow(ErrMissingArch);
  });

  it('validates the base URL', () => {
    const bad = new Client({ baseURL: 'not-a-url' });
    expect(() => bad.buildNativeFeedURL(feedOptions())).toThrow(ErrInvalidBaseURL);
  });

  it('exposes the set of supported native updaters', () => {
    expect([...NATIVE_UPDATERS].sort()).toEqual(['squirrel_darwin', 'squirrel_windows']);
  });
});

describe('Client.resolveNativeFeed', () => {
  it('serves an available update from the edge without touching the API', async () => {
    let apiCalled = false;
    const apiServer = await createTestServer((_, res) => {
      apiCalled = true;
      res.writeHead(500);
      res.end();
    });
    const edgeServer = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      expect(url.pathname).toBe(
        '/responses/admin/demo/nightly/darwin/arm64/squirrel_darwin/0.0.1.json',
      );
      writeJSON(res, { url: 'https://cdn.example/app-0.0.2.zip' });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const feed = await client.resolveNativeFeed(feedOptions());
      expect(feed.updateAvailable).toBe(true);
      expect(feed.source).toBe('edge');
      expect(feed.url).toBe('https://cdn.example/app-0.0.2.zip');
      expect(feed.feedURL).toBe(`${edgeServer.url}/responses/admin/demo/nightly/darwin/arm64/squirrel_darwin/0.0.1.json`);
      expect(apiCalled).toBe(false);
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it('treats edge no_content as "no update" without touching the API', async () => {
    let apiCalled = false;
    const apiServer = await createTestServer((_, res) => {
      apiCalled = true;
      res.writeHead(500);
      res.end();
    });
    const edgeServer = await createTestServer((_, res) => {
      writeJSON(res, { status: 'no_content' });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const feed = await client.resolveNativeFeed(feedOptions());
      expect(feed.updateAvailable).toBe(false);
      expect(feed.source).toBe('edge');
      expect(feed.url).toBeUndefined();
      expect(apiCalled).toBe(false);
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it('falls back to the API when the edge object is missing', async () => {
    const apiServer = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      expect(url.pathname).toBe('/checkVersion');
      expect(url.searchParams.get('updater')).toBe('squirrel_darwin');
      writeJSON(res, { url: 'https://cdn.example/app-0.0.2.zip' });
    });
    const edgeServer = await createTestServer((_, res) => {
      res.writeHead(404);
      res.end();
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const feed = await client.resolveNativeFeed(feedOptions());
      expect(feed.updateAvailable).toBe(true);
      expect(feed.source).toBe('api');
      expect(feed.feedURL.startsWith(`${apiServer.url}/checkVersion`)).toBe(true);
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it('probes the API directly when no edge URL is configured', async () => {
    const apiServer = await createTestServer((_, res) => {
      res.writeHead(204);
      res.end();
    });

    try {
      const client = new Client({ baseURL: apiServer.url });
      const feed = await client.resolveNativeFeed(feedOptions());
      expect(feed.updateAvailable).toBe(false);
      expect(feed.source).toBe('api');
    } finally {
      await apiServer.close();
    }
  });

  it('resolves squirrel_windows from the edge to the RELEASES directory (no /RELEASES suffix)', async () => {
    let apiCalled = false;
    const apiServer = await createTestServer((_, res) => {
      apiCalled = true;
      res.writeHead(500);
      res.end();
    });
    const releasesURL =
      'https://cdn.example/squirrel_windows/demo-admin/0.0.2/nightly/win32/x64/RELEASES';
    const edgeServer = await createTestServer((req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      expect(url.pathname).toBe(
        '/responses/admin/demo/nightly/win32/x64/squirrel_windows/0.0.1.json',
      );
      writeJSON(res, { status: 'redirect', url: releasesURL });
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const feed = await client.resolveNativeFeed(
        feedOptions({ updater: 'squirrel_windows', platform: 'win32', arch: 'x64' }),
      );
      expect(feed.updateAvailable).toBe(true);
      expect(feed.source).toBe('edge');
      expect(feed.url).toBe(releasesURL);
      expect(feed.feedURL).toBe(
        'https://cdn.example/squirrel_windows/demo-admin/0.0.2/nightly/win32/x64',
      );
      expect(apiCalled).toBe(false);
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it('falls back to the /update base for squirrel_windows when the edge is cold', async () => {
    const apiServer = await createTestServer((_, res) => {
      res.writeHead(500);
      res.end();
    });
    const edgeServer = await createTestServer((_, res) => {
      res.writeHead(404);
      res.end();
    });

    try {
      const client = new Client({ baseURL: apiServer.url, edgeURL: edgeServer.url });
      const feed = await client.resolveNativeFeed(
        feedOptions({ updater: 'squirrel_windows', platform: 'win32', arch: 'x64' }),
      );
      expect(feed.updateAvailable).toBe(true);
      expect(feed.source).toBe('api');
      expect(feed.feedURL).toBe(`${apiServer.url}/update/admin/demo/nightly/win32/x64/0.0.1`);
    } finally {
      await Promise.all([apiServer.close(), edgeServer.close()]);
    }
  });

  it('returns the /update base for squirrel_windows without an edge URL and no network', async () => {
    let called = false;
    const apiServer = await createTestServer((_, res) => {
      called = true;
      res.writeHead(500);
      res.end();
    });

    try {
      const client = new Client({ baseURL: apiServer.url });
      const feed = await client.resolveNativeFeed(
        feedOptions({ updater: 'squirrel_windows', platform: 'win32', arch: 'x64' }),
      );
      expect(feed.updateAvailable).toBe(true);
      expect(feed.source).toBe('api');
      expect(feed.feedURL).toBe(`${apiServer.url}/update/admin/demo/nightly/win32/x64/0.0.1`);
      expect(called).toBe(false);
    } finally {
      await apiServer.close();
    }
  });
});
