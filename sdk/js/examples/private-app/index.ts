import { createWriteStream } from 'node:fs';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { extname, join } from 'node:path';
import { Readable } from 'node:stream';
import { pipeline } from 'node:stream/promises';
import type { ReadableStream } from 'node:stream/web';
import {
  Client,
  DOWNLOAD_TOKEN_HEADER,
  RequestFailedError,
  systemArch,
  systemPlatform,
} from '../../src';
import type { UpdateResponse, UpdateSource } from '../../src';

// Only a private app in strict mode needs a token; an unlisted one is served without it. The token belongs
// to one app and one channel, and it is an identifier baked into a build rather than a secret: for an
// internal tool keep it out of the bundle and read it from the environment, as here.
const token = process.env.DPAPPREGISTRY_DOWNLOAD_TOKEN ?? '';

const client = new Client({
  baseURL: 'http://localhost:9000',
});

async function main(): Promise<void> {
  if (token === '') {
    console.log('DPAPPREGISTRY_DOWNLOAD_TOKEN is not set: this works for a public or unlisted app, a strict one will answer as if the app did not exist');
  }

  let resp: UpdateResponse;
  try {
    resp = await client.checkForUpdates({
      owner: 'admin',
      appName: 'internal-tool',
      version: '0.0.0.1',
      channel: 'stable',
      platform: systemPlatform(),
      arch: systemArch(),
      downloadToken: token || undefined,
    });
  } catch (err) {
    if (err instanceof RequestFailedError) {
      if (token === '') {
        console.error('a strict private app answers an update check without a token exactly as it answers one for an app that does not exist');
      }
      console.error('update check request failed:', err.message);
    } else {
      console.error('invalid update check options:', (err as Error).message);
    }
    process.exit(1);
  }

  printUpdateResponse(resp);

  if (!resp.updateAvailable || resp.updateUrl === '') {
    return;
  }

  try {
    const path = await downloadArtifact(resp.updateUrl);
    console.log(`\nDownloaded to ${path}`);
  } catch (err) {
    console.error('download failed:', (err as Error).message);
    process.exit(1);
  }
}

// downloadArtifact fetches the artifact the update check pointed at. The SDK never downloads anything, so the
// header is set here as well. /download redirects to presigned storage and fetch would carry the header along,
// so the redirect is followed by hand, without the token.
async function downloadArtifact(updateUrl: string): Promise<string> {
  const headers: Record<string, string> = {};
  if (token !== '') {
    headers[DOWNLOAD_TOKEN_HEADER] = token;
  }

  let res = await fetch(updateUrl, { headers, redirect: 'manual' });
  const location = res.headers.get('location');
  if (res.status >= 300 && res.status < 400 && location !== null) {
    await res.body?.cancel();
    res = await fetch(new URL(location, updateUrl));
  }

  if (res.status !== 200 || res.body === null) {
    // A missing token on a strict app, or one that does not cover this app and channel, is answered
    // with the same 404 as a key that belongs to no artifact.
    await res.body?.cancel();
    throw new Error(`unexpected status ${res.status}`);
  }

  const dir = await mkdtemp(join(tmpdir(), 'dpappregistry-'));
  const path = join(dir, `artifact${extname(new URL(updateUrl).pathname)}`);
  await pipeline(Readable.fromWeb(res.body as ReadableStream<Uint8Array>), createWriteStream(path));
  return path;
}

function printUpdateResponse(resp: UpdateResponse): void {
  const pad = '  ';

  console.log('-- Update check response --');
  console.log(`${pad}update_available:         ${resp.updateAvailable}`);
  console.log(`${pad}critical:                 ${resp.critical}`);
  console.log(`${pad}is_intermediate_required: ${resp.isIntermediateRequired}`);
  console.log(`${pad}possible_rollback:        ${resp.possibleRollback}`);
  console.log(`${pad}source:                   ${formatUpdateSource(resp.source)}`);

  if (resp.updateUrl !== '') {
    console.log(`${pad}update_url:               ${resp.updateUrl}`);
  } else {
    console.log(`${pad}update_url:               (empty)`);
  }

  if (resp.changelog !== '') {
    console.log(`${pad}changelog:`);
    for (const line of resp.changelog.trimEnd().split('\n')) {
      console.log(`${pad}  ${line}`);
    }
  } else {
    console.log(`${pad}changelog:               (empty)`);
  }

  if (resp.packageUrls.length === 0) {
    console.log(`${pad}package_urls:           (none)`);
    return;
  }

  console.log(`${pad}package_urls:`);
  for (const pkg of resp.packageUrls) {
    console.log(`${pad}  ${pkg.package.padEnd(12)} ${pkg.url}`);
  }
}

function formatUpdateSource(source: UpdateSource): string {
  switch (source) {
    case 'edge': return 'edge';
    case 'api':  return 'api';
    default:     return 'unknown';
  }
}

main();
