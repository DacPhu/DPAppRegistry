import { gzipSync } from 'node:zlib';
import type {
  CheckOptions,
  NativeFeedOptions,
  NativeFeedResult,
  PackageUpdateURL,
  ReportEventType,
  ReportOptions,
  ReportResponse,
  RolloutInfo,
  UpdateResponse,
  UpdateSource,
} from './types';
import { evaluateRollout } from './rollout';
import {
  buildCheckVersionURL,
  buildNativeFeedURL as buildNativeFeedURLFor,
  isEdgeCacheableUpdater,
  nativeFeedKind,
  stripReleasesSuffix,
  type NativeFeedKind,
} from './feed';
import { joinURLPath, parseAbsoluteURL } from './url';
import {
  CheckError,
  EndpointError,
  ErrInvalidBaseURL,
  ErrInvalidEdgeURL,
  ErrInvalidEventType,
  ErrInvalidReason,
  ErrMissingAppName,
  ErrMissingArch,
  ErrMissingBaseURL,
  ErrMissingChannel,
  ErrMissingDeviceId,
  ErrMissingOwner,
  ErrMissingPlatform,
  ErrMissingReportKey,
  ErrMissingVersion,
} from './errors';

const DEFAULT_TIMEOUT_MS = 30_000;
const USER_AGENT = 'dpappregistry-js/1.0';
const UPDATER = 'manual';

export const DOWNLOAD_TOKEN_HEADER = 'X-Download-Token';

export interface Config {
  readonly baseURL: string;
  readonly edgeURL?: string;
  readonly fetch?: typeof globalThis.fetch;
  readonly timeoutMs?: number;
}

interface RawUpdateResponse {
  update_available?: boolean;
  update_url?: string;
  changelog?: string;
  critical?: boolean;
  is_intermediate_required?: boolean;
  possible_rollback?: boolean;
  rollout?: { percent?: number; seed?: string };
  [key: string]: unknown;
}

interface RawReportResponse {
  status?: string;
  group_hash?: string;
  stored_details?: boolean;
}

interface ReportRequestDetails {
  encoding: string;
  content_type: string;
  payload: string;
}

interface ReportRequestBody {
  application: { name: string; version: string; channel: string };
  system: { platform: string; arch: string };
  event: { type: ReportEventType; reason: string };
  details?: ReportRequestDetails;
}

const REPORT_EVENT_TYPES: readonly ReportEventType[] = [
  'crash',
  'startup_failure',
  'update_failure',
  'install_failure',
  'rollback_failure',
];

const REPORT_REASON_PATTERN = /^[a-zA-Z0-9._-]{1,128}$/;

export class Client {
  private readonly baseURL: string;
  private readonly edgeURL: string;
  private readonly fetchFn: typeof globalThis.fetch;
  private readonly timeoutMs: number;

  constructor(cfg: Config) {
    this.baseURL = cfg.baseURL;
    this.edgeURL = cfg.edgeURL ?? '';
    this.fetchFn = cfg.fetch ?? globalThis.fetch.bind(globalThis);
    this.timeoutMs = cfg.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  }

  async checkForUpdates(opts: CheckOptions, signal?: AbortSignal): Promise<UpdateResponse> {
    this.validateConfig();
    validateCheckOptions(opts);

    const sig = signal ?? null;
    let edgeError: Error | undefined;

    // A private app is never published to the edge, so with a token the lookup could only miss.
    if (this.edgeURL !== '' && !opts.downloadToken) {
      try {
        const resp = await this.checkEdge(opts, sig);
        if (opts.deviceId) {
          try {
            await this.sendEdgeTelemetryBeacon(opts, !resp.updateAvailable, sig);
          } catch {
            // telemetry failures don't affect the update check result
          }
        }
        return { ...resp, source: 'edge' };
      } catch (err) {
        edgeError = err as Error;
        if (signal?.aborted === true) {
          throw new CheckError(edgeError);
        }
      }
    }

    try {
      const resp = await this.checkAPI(opts, sig);
      return { ...resp, source: 'api' };
    } catch (apiError) {
      throw new CheckError(edgeError, apiError as Error);
    }
  }

  async reportEvent(opts: ReportOptions, signal?: AbortSignal): Promise<ReportResponse> {
    this.validateConfig();
    validateReportOptions(opts);

    const url = this.buildReportURL();
    const body = buildReportBody(opts);

    const headers: Record<string, string> = {
      Authorization: `Bearer ${opts.reportKey}`,
      'X-Device-ID': opts.deviceId,
      'Content-Type': 'application/json',
      Accept: 'application/json',
      'User-Agent': USER_AGENT,
    };

    const reqSignal = createRequestSignal(signal ?? null, this.timeoutMs);

    let res: Response;
    try {
      res = await this.fetchFn(url, {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
        signal: reqSignal,
      });
    } catch (err) {
      throw new EndpointError('report', url, undefined, err as Error);
    }

    if (res.status !== 202) {
      await res.body?.cancel();
      throw new EndpointError('report', url, res.status);
    }

    let raw: RawReportResponse;
    try {
      raw = (await res.json()) as RawReportResponse;
    } catch (err) {
      throw new EndpointError('report', url, undefined, err as Error);
    }

    return parseReportResponse(raw);
  }

  private buildReportURL(): string {
    const u = parseAbsoluteURL(this.baseURL, ErrInvalidBaseURL);
    u.pathname = joinURLPath(u.pathname, 'reports/ingest');
    return u.toString();
  }

  private validateConfig(): void {
    if (this.baseURL.trim() === '') {
      throw ErrMissingBaseURL;
    }
    parseAbsoluteURL(this.baseURL, ErrInvalidBaseURL);
  }

  private async checkEdge(opts: CheckOptions, signal: AbortSignal | null): Promise<UpdateResponse> {
    const url = this.buildEdgeCheckURL(opts);
    return this.doUpdateRequest(url, opts, 'edge', signal);
  }

  private async checkAPI(opts: CheckOptions, signal: AbortSignal | null): Promise<UpdateResponse> {
    const url = this.buildAPICheckURL(opts);
    return this.doUpdateRequest(url, opts, 'api', signal);
  }

  private async doUpdateRequest(
    url: string,
    opts: CheckOptions,
    source: UpdateSource,
    signal: AbortSignal | null,
  ): Promise<UpdateResponse> {
    const headers: Record<string, string> = {
      Accept: 'application/json',
      'User-Agent': USER_AGENT,
    };
    if (opts.deviceId) {
      headers['X-Device-ID'] = opts.deviceId;
    }
    if (opts.downloadToken) {
      headers[DOWNLOAD_TOKEN_HEADER] = opts.downloadToken;
    }

    const reqSignal = createRequestSignal(signal, this.timeoutMs);

    let res: Response;
    try {
      res = await this.fetchFn(url, { headers, signal: reqSignal });
    } catch (err) {
      throw new EndpointError(source, url, undefined, err as Error);
    }

    if (res.status !== 200) {
      await res.body?.cancel();
      throw new EndpointError(source, url, res.status);
    }

    let raw: RawUpdateResponse;
    try {
      raw = (await res.json()) as RawUpdateResponse;
    } catch (err) {
      throw new EndpointError(source, url, undefined, err as Error);
    }

    return parseUpdateResponse(raw, opts.deviceId);
  }

  buildNativeFeedURL(opts: NativeFeedOptions): string {
    this.validateConfig();
    return buildNativeFeedURLFor(this.baseURL, opts);
  }

  async resolveNativeFeed(opts: NativeFeedOptions, signal?: AbortSignal): Promise<NativeFeedResult> {
    this.validateConfig();
    const directURL = this.buildNativeFeedURL(opts);
    const sig = signal ?? null;
    const kind = nativeFeedKind(opts.updater);

    if (isEdgeCacheableUpdater(opts.updater) && this.edgeURL !== '') {
      const edgeURL = this.buildEdgeResponseURL(opts, opts.updater);
      try {
        const r = await this.fetchNativeFeed(edgeURL, 'edge', sig);
        return toNativeFeedResult(kind, r, edgeURL, directURL, 'edge');
      } catch {
        // edge miss/error -> fall back to the API feed
      }
    }

    if (kind === 'releasesDir') {
      // The /update base serves RELEASES (with absolute package URLs) and warms the edge;
      // Squirrel.Windows diffs RELEASES itself, so no pre-check is needed.
      return { updateAvailable: true, feedURL: directURL, source: 'api' };
    }

    const r = await this.fetchNativeFeed(directURL, 'api', sig);
    return toNativeFeedResult(kind, r, directURL, directURL, 'api');
  }

  private async fetchNativeFeed(
    url: string,
    source: UpdateSource,
    signal: AbortSignal | null,
  ): Promise<{ updateAvailable: boolean; url?: string }> {
    const headers: Record<string, string> = {
      Accept: 'application/json',
      'User-Agent': USER_AGENT,
    };
    const reqSignal = createRequestSignal(signal, this.timeoutMs);

    let res: Response;
    try {
      res = await this.fetchFn(url, { headers, signal: reqSignal });
    } catch (err) {
      throw new EndpointError(source, url, undefined, err as Error);
    }

    if (res.status === 204) {
      await res.body?.cancel();
      return { updateAvailable: false };
    }
    if (res.status !== 200) {
      await res.body?.cancel();
      throw new EndpointError(source, url, res.status);
    }

    let raw: { url?: unknown };
    try {
      raw = (await res.json()) as { url?: unknown };
    } catch (err) {
      throw new EndpointError(source, url, undefined, err as Error);
    }

    const feedUrl = typeof raw.url === 'string' ? raw.url : undefined;
    return { updateAvailable: feedUrl !== undefined, url: feedUrl };
  }

  private buildAPICheckURL(opts: CheckOptions): string {
    return buildCheckVersionURL(this.baseURL, opts, UPDATER);
  }

  private buildEdgeCheckURL(opts: CheckOptions): string {
    return this.buildEdgeResponseURL(opts, UPDATER);
  }

  private buildEdgeResponseURL(opts: CheckOptions, updater: string): string {
    const u = parseAbsoluteURL(this.edgeURL, ErrInvalidEdgeURL);
    // Mirrors the server's CDN object key: empty dimensions are left out, and without a
    // platform the server resolves no updater, so that segment is dropped as well.
    const platform = opts.platform ?? '';
    const dimensions = [opts.channel ?? '', platform, opts.arch ?? '', platform === '' ? '' : updater];
    const segments = [
      'responses',
      opts.owner,
      opts.appName,
      ...dimensions.filter((segment) => segment !== ''),
      `${opts.version.replace(/-/g, '.')}.json`,
    ];
    const base = (u.origin + u.pathname).replace(/\/+$/, '');
    return `${base}/${segments.map(encodeURIComponent).join('/')}`;
  }

  private buildEdgeTelemetryBeaconURL(opts: CheckOptions, isLatest: boolean): string {
    const u = parseAbsoluteURL(this.baseURL, ErrInvalidBaseURL);
    u.pathname = joinURLPath(u.pathname, 'telemetry/beacon');
    u.search = new URLSearchParams({
      app_name: opts.appName,
      version: opts.version,
      channel: opts.channel ?? '',
      platform: opts.platform ?? '',
      arch: opts.arch ?? '',
      owner: opts.owner,
      is_latest: String(isLatest),
    }).toString();
    return u.toString();
  }

  private async sendEdgeTelemetryBeacon(
    opts: CheckOptions,
    isLatest: boolean,
    signal: AbortSignal | null,
  ): Promise<void> {
    const url = this.buildEdgeTelemetryBeaconURL(opts, isLatest);
    const headers: Record<string, string> = { 'User-Agent': USER_AGENT };
    if (opts.deviceId) {
      headers['X-Device-ID'] = opts.deviceId;
    }

    const reqSignal = createRequestSignal(signal, this.timeoutMs);
    const res = await this.fetchFn(url, { headers, signal: reqSignal });
    await res.body?.cancel();
    if (res.status !== 200) {
      throw new EndpointError('edge', url, res.status);
    }
  }
}

function validateCheckOptions(opts: CheckOptions): void {
  if (!opts.owner) throw ErrMissingOwner;
  if (!opts.appName) throw ErrMissingAppName;
  if (!opts.version) throw ErrMissingVersion;
}

function validateReportOptions(opts: ReportOptions): void {
  if (!opts.reportKey) throw ErrMissingReportKey;
  if (!opts.deviceId) throw ErrMissingDeviceId;
  if (!opts.appName) throw ErrMissingAppName;
  if (!opts.version) throw ErrMissingVersion;
  if (!opts.channel) throw ErrMissingChannel;
  if (!opts.platform) throw ErrMissingPlatform;
  if (!opts.arch) throw ErrMissingArch;
  if (!REPORT_EVENT_TYPES.includes(opts.event.type)) throw ErrInvalidEventType;
  if (!REPORT_REASON_PATTERN.test(opts.event.reason)) throw ErrInvalidReason;
}

function buildReportBody(opts: ReportOptions): ReportRequestBody {
  const body: ReportRequestBody = {
    application: { name: opts.appName, version: opts.version, channel: opts.channel },
    system: { platform: opts.platform, arch: opts.arch },
    event: { type: opts.event.type, reason: opts.event.reason },
  };

  if (opts.details !== undefined) {
    const payload = gzipSync(Buffer.from(JSON.stringify(opts.details))).toString('base64');
    body.details = {
      encoding: 'gzip+base64',
      content_type: 'application/json',
      payload,
    };
  }

  return body;
}

function parseReportResponse(raw: RawReportResponse): ReportResponse {
  return {
    status: raw.status ?? '',
    groupHash: raw.group_hash ?? '',
    storedDetails: raw.stored_details ?? false,
  };
}

function toNativeFeedResult(
  kind: NativeFeedKind,
  r: { updateAvailable: boolean; url?: string },
  jsonFeedURL: string,
  releasesDirFallback: string,
  source: UpdateSource,
): NativeFeedResult {
  if (kind === 'releasesDir') {
    if (r.updateAvailable && r.url !== undefined) {
      return { updateAvailable: true, feedURL: stripReleasesSuffix(r.url), source, url: r.url };
    }
    return { updateAvailable: false, feedURL: releasesDirFallback, source };
  }
  // 'json' — the framework reads the JSON feed at jsonFeedURL directly
  return { updateAvailable: r.updateAvailable, feedURL: jsonFeedURL, source, url: r.url };
}

function createRequestSignal(userSignal: AbortSignal | null, timeoutMs: number): AbortSignal {
  const timeoutSignal = AbortSignal.timeout(timeoutMs);
  if (userSignal === null) return timeoutSignal;

  if (userSignal.aborted) {
    const c = new AbortController();
    c.abort(userSignal.reason);
    return c.signal;
  }

  const controller = new AbortController();
  const abort = (reason: unknown) => controller.abort(reason);
  userSignal.addEventListener('abort', () => abort(userSignal.reason), { once: true });
  timeoutSignal.addEventListener('abort', () => abort(timeoutSignal.reason), { once: true });
  return controller.signal;
}

function parseUpdateResponse(
  raw: RawUpdateResponse,
  deviceId: string | undefined,
): UpdateResponse {
  const packageUrls: PackageUpdateURL[] = [];

  for (const [key, value] of Object.entries(raw)) {
    if (key.startsWith('update_url_') && typeof value === 'string') {
      packageUrls.push({ package: key.slice('update_url_'.length), url: value });
    }
  }

  packageUrls.sort((a, b) => a.package.localeCompare(b.package));

  let updateAvailable = raw.update_available ?? false;
  let rollout: RolloutInfo | undefined;
  let gated = false;

  const rawRollout = raw.rollout;
  if (rawRollout && typeof rawRollout.percent === 'number' && typeof rawRollout.seed === 'string') {
    rollout = evaluateRollout(rawRollout.percent, rawRollout.seed, deviceId);
    if (updateAvailable && !rollout.eligible) {
      updateAvailable = false;
      gated = true;
    }
  }

  // When the rollout excludes this install we blank the download URLs too, so a caller
  // that inspects `updateUrl`/`packageUrls` instead of `updateAvailable` cannot bypass
  // the gate and pull an update the device was not offered.
  return {
    updateAvailable,
    updateUrl: gated ? '' : raw.update_url ?? '',
    changelog: raw.changelog ?? '',
    critical: raw.critical ?? false,
    isIntermediateRequired: raw.is_intermediate_required ?? false,
    possibleRollback: raw.possible_rollback ?? false,
    packageUrls: gated ? [] : packageUrls,
    source: 'unknown',
    rollout,
  };
}
