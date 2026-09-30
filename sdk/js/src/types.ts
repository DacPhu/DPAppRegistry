export type UpdateSource = 'unknown' | 'edge' | 'api';

export type EndpointSource = UpdateSource | 'report';

export interface PackageUpdateURL {
  readonly package: string;
  readonly url: string;
}

export interface CheckOptions {
  readonly owner: string;
  readonly appName: string;
  readonly version: string;
  readonly channel?: string;
  readonly platform?: string;
  readonly arch?: string;
  readonly deviceId?: string;
  // Download token of a private app in strict mode, scoped to one app and channel. Such an app answers a
  // check without it exactly as it answers a check for an unknown app.
  readonly downloadToken?: string;
}

export type NativeUpdater = 'squirrel_darwin' | 'squirrel_windows';

export interface NativeFeedOptions extends CheckOptions {
  readonly updater: NativeUpdater;
}

export interface NativeFeedResult {
  readonly updateAvailable: boolean;
  // URL to hand to the native updater (autoUpdater.setFeedURL): the edge object when
  // it served the feed, otherwise the API endpoint.
  readonly feedURL: string;
  readonly source: UpdateSource;
  readonly url?: string;
}

export interface RolloutInfo {
  readonly percent: number;
  readonly seed: string;
  // Deterministic bucket in [0, 99] for this device, or null when no deviceId was
  // supplied and the bucket could not be computed.
  readonly bucket: number | null;
  readonly eligible: boolean;
}

export interface UpdateResponse {
  readonly updateAvailable: boolean;
  readonly updateUrl: string;
  readonly changelog: string;
  readonly critical: boolean;
  readonly isIntermediateRequired: boolean;
  readonly possibleRollback: boolean;
  readonly packageUrls: readonly PackageUpdateURL[];
  readonly source: UpdateSource;
  // Present only when the server offered a staged (canary) rollout for the version.
  // When `eligible` is false the SDK has already forced `updateAvailable` to false.
  readonly rollout?: RolloutInfo;
}

export type ReportEventType =
  | 'crash'
  | 'startup_failure'
  | 'update_failure'
  | 'install_failure'
  | 'rollback_failure';

export interface ReportEvent {
  readonly type: ReportEventType;
  readonly reason: string;
}

export interface ReportOptions {
  readonly reportKey: string;
  readonly deviceId: string;
  readonly appName: string;
  readonly version: string;
  readonly channel: string;
  readonly platform: string;
  readonly arch: string;
  readonly event: ReportEvent;
  readonly details?: Readonly<Record<string, unknown>>;
}

export interface ReportResponse {
  readonly status: string;
  readonly groupHash: string;
  readonly storedDetails: boolean;
}
