import https from 'https';
import { Client, RequestFailedError } from '../../src';
import type { UpdateResponse, UpdateSource } from '../../src';

const DEVICE_ID = 'device-1';

const keepAliveAgent = new https.Agent({
  keepAlive: true,
  maxSockets: 10,
  timeout: 5_000,
});

const customFetch: typeof globalThis.fetch = (input, init) => {
  return globalThis.fetch(input, {
    ...init,
    agent: keepAliveAgent,
  });
};

const client = new Client({
  baseURL: 'http://localhost:9000',
  fetch: customFetch,
  timeoutMs: 10_000,
});

async function main(): Promise<void> {
  let resp: UpdateResponse;
  try {
    resp = await client.checkForUpdates({
      owner: 'admin',
      appName: 'test',
      version: '0.0.0.1',
      channel: 'nightly',
      platform: 'darwin',
      arch: 'arm64',
      deviceId: DEVICE_ID,
    });
  } catch (err) {
    if (err instanceof RequestFailedError) {
      console.error('update check request failed:', err.message);
    } else {
      console.error('invalid update check options:', (err as Error).message);
    }
    process.exit(1);
  }

  printUpdateResponse(resp);
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
