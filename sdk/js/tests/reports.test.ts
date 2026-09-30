import http from 'http';
import { gunzipSync } from 'node:zlib';
import { Client } from '../src/client';
import {
  EndpointError,
  ErrInvalidEventType,
  ErrInvalidReason,
  ErrMissingAppName,
  ErrMissingArch,
  ErrMissingChannel,
  ErrMissingDeviceId,
  ErrMissingPlatform,
  ErrMissingReportKey,
  ErrMissingVersion,
} from '../src/errors';
import type { ReportOptions } from '../src/types';
import { createTestServer, writeJSON } from './helpers';

function defaultOptions(): ReportOptions {
  return {
    reportKey: 'rpk_test',
    deviceId: 'device-1',
    appName: 'test',
    version: '0.0.0.5',
    channel: 'nightly',
    platform: 'darwin',
    arch: 'arm64',
    event: { type: 'crash', reason: 'segfault.signal-11' },
  };
}

function readBody(req: http.IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    let data = '';
    req.on('data', (chunk) => {
      data += chunk;
    });
    req.on('end', () => resolve(data));
    req.on('error', reject);
  });
}

describe('Client.reportEvent', () => {
  it('posts to /reports/ingest with correct headers and body, returns mapped response', async () => {
    let captured: { method?: string; pathname?: string; headers: http.IncomingHttpHeaders; body: unknown } = {
      headers: {},
      body: undefined,
    };

    const server = await createTestServer(async (req, res) => {
      const url = new URL(req.url!, `http://${req.headers.host}`);
      const raw = await readBody(req);
      captured = { method: req.method, pathname: url.pathname, headers: req.headers, body: JSON.parse(raw) };
      res.statusCode = 202;
      writeJSON(res, { status: 'accepted', group_hash: 'abc123', stored_details: false });
    });

    try {
      const client = new Client({ baseURL: server.url });
      const resp = await client.reportEvent(defaultOptions());

      expect(captured.method).toBe('POST');
      expect(captured.pathname).toBe('/reports/ingest');
      expect(captured.headers['authorization']).toBe('Bearer rpk_test');
      expect(captured.headers['x-device-id']).toBe('device-1');
      expect(captured.headers['content-type']).toContain('application/json');
      expect(captured.headers['accept']).toContain('application/json');
      expect(captured.headers['user-agent']).toBeTruthy();

      expect(captured.body).toEqual({
        application: { name: 'test', version: '0.0.0.5', channel: 'nightly' },
        system: { platform: 'darwin', arch: 'arm64' },
        event: { type: 'crash', reason: 'segfault.signal-11' },
      });

      expect(resp).toEqual({ status: 'accepted', groupHash: 'abc123', storedDetails: false });
    } finally {
      await server.close();
    }
  });

  it('omits details when no details object is supplied', async () => {
    let body: { details?: unknown } = {};
    const server = await createTestServer(async (req, res) => {
      body = JSON.parse(await readBody(req));
      res.statusCode = 202;
      writeJSON(res, { status: 'accepted', group_hash: 'h', stored_details: false });
    });

    try {
      const client = new Client({ baseURL: server.url });
      await client.reportEvent(defaultOptions());
      expect('details' in body).toBe(false);
    } finally {
      await server.close();
    }
  });

  it('encodes details as gzip+base64 and round-trips the payload', async () => {
    const details = { stack: 'at foo()\nat bar()', code: 11, nested: { a: [1, 2, 3] } };
    let body: { details?: { encoding: string; content_type: string; payload: string } } = {};

    const server = await createTestServer(async (req, res) => {
      body = JSON.parse(await readBody(req));
      res.statusCode = 202;
      writeJSON(res, { status: 'accepted', group_hash: 'h', stored_details: true });
    });

    try {
      const client = new Client({ baseURL: server.url });
      const resp = await client.reportEvent({ ...defaultOptions(), details });

      expect(body.details?.encoding).toBe('gzip+base64');
      expect(body.details?.content_type).toBe('application/json');

      const decoded = gunzipSync(Buffer.from(body.details!.payload, 'base64')).toString('utf8');
      expect(JSON.parse(decoded)).toEqual(details);
      expect(decoded).toBe(JSON.stringify(details));
      expect(resp.storedDetails).toBe(true);
    } finally {
      await server.close();
    }
  });

  it('defaults missing response fields', async () => {
    const server = await createTestServer((_, res) => {
      res.statusCode = 202;
      writeJSON(res, {});
    });

    try {
      const client = new Client({ baseURL: server.url });
      const resp = await client.reportEvent(defaultOptions());
      expect(resp).toEqual({ status: '', groupHash: '', storedDetails: false });
    } finally {
      await server.close();
    }
  });

  describe('input validation', () => {
    it.each<{ name: string; opts: ReportOptions; want: Error }>([
      { name: 'missing report key', opts: { ...defaultOptions(), reportKey: '' }, want: ErrMissingReportKey },
      { name: 'missing device id', opts: { ...defaultOptions(), deviceId: '' }, want: ErrMissingDeviceId },
      { name: 'missing app name', opts: { ...defaultOptions(), appName: '' }, want: ErrMissingAppName },
      { name: 'missing version', opts: { ...defaultOptions(), version: '' }, want: ErrMissingVersion },
      { name: 'missing channel', opts: { ...defaultOptions(), channel: '' }, want: ErrMissingChannel },
      { name: 'missing platform', opts: { ...defaultOptions(), platform: '' }, want: ErrMissingPlatform },
      { name: 'missing arch', opts: { ...defaultOptions(), arch: '' }, want: ErrMissingArch },
      {
        name: 'invalid event type',
        opts: { ...defaultOptions(), event: { type: 'boom' as never, reason: 'x' } },
        want: ErrInvalidEventType,
      },
      {
        name: 'empty reason',
        opts: { ...defaultOptions(), event: { type: 'crash', reason: '' } },
        want: ErrInvalidReason,
      },
      {
        name: 'reason with invalid chars',
        opts: { ...defaultOptions(), event: { type: 'crash', reason: 'bad reason!' } },
        want: ErrInvalidReason,
      },
      {
        name: 'reason too long',
        opts: { ...defaultOptions(), event: { type: 'crash', reason: 'a'.repeat(129) } },
        want: ErrInvalidReason,
      },
    ])('rejects with $name error', async ({ opts, want }) => {
      const client = new Client({ baseURL: 'https://api.example.com' });
      await expect(client.reportEvent(opts)).rejects.toBe(want);
    });
  });

  it.each([401, 403, 429, 500])('throws EndpointError with statusCode on HTTP %s', async (status) => {
    const server = await createTestServer((_, res) => {
      res.writeHead(status);
      res.end();
    });

    try {
      const client = new Client({ baseURL: server.url });
      const err = await client.reportEvent(defaultOptions()).catch((e) => e);
      expect(err).toBeInstanceOf(EndpointError);
      expect((err as EndpointError).source).toBe('report');
      expect((err as EndpointError).statusCode).toBe(status);
    } finally {
      await server.close();
    }
  });

  it('wraps a fetch network error in EndpointError', async () => {
    const networkError = new TypeError('fetch failed');
    const client = new Client({
      baseURL: 'http://api.example.com',
      fetch: () => Promise.reject(networkError),
    });

    const err = await client.reportEvent(defaultOptions()).catch((e) => e);
    expect(err).toBeInstanceOf(EndpointError);
    expect((err as EndpointError).source).toBe('report');
    expect((err as EndpointError).cause).toBe(networkError);
  });

  it('builds correct URL when baseURL has a path prefix', async () => {
    let pathname = '';
    const server = await createTestServer((req, res) => {
      pathname = new URL(req.url!, `http://${req.headers.host}`).pathname;
      res.statusCode = 202;
      writeJSON(res, { status: 'accepted', group_hash: 'h', stored_details: false });
    });

    try {
      const client = new Client({ baseURL: `${server.url}/v1` });
      await client.reportEvent(defaultOptions());
      expect(pathname).toBe('/v1/reports/ingest');
    } finally {
      await server.close();
    }
  });
});
