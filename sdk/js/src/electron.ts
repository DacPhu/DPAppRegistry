// Update checks for an Electron app against DPAppRegistry: the flow every
// Electron product shares, so an app only passes in its identity. It checks
// shortly after launch, then on an interval, and on demand (checkNow, for a
// "Check for Updates…" item). When a release carries an electron-builder feed
// (its latest*.yml package) and this install can replace itself, the feed's
// folder goes to electron-updater, which downloads and installs; otherwise the
// platform's download opens in the browser.
//
// The app passes in its electron modules and electron-updater's autoUpdater,
// so this package depends on neither. Import it as
// "@dacphu/dpappregistry-sdk/electron".

import { execFile } from 'child_process';
import * as path from 'path';
import type { Client } from './client';
import { systemArch, systemPlatform } from './system';
import type { CheckOptions, UpdateResponse } from './types';

interface MessageBoxOptions {
  type?: 'info' | 'warning' | 'error';
  buttons?: string[];
  defaultId?: number;
  cancelId?: number;
  message: string;
  detail?: string;
}

/** The parts of Electron the helper uses: pass `require('electron')`. */
export interface ElectronModules {
  readonly app: { getVersion(): string; readonly isPackaged: boolean };
  readonly dialog: { showMessageBox(options: MessageBoxOptions): Promise<{ response: number }> };
  readonly shell: { openExternal(url: string): Promise<void> };
}

/** The parts of electron-updater's autoUpdater the helper uses. */
export interface AutoUpdaterLike {
  autoDownload: boolean;
  autoInstallOnAppQuit: boolean;
  setFeedURL(options: { provider: 'generic'; url: string }): void;
  checkForUpdates(): Promise<{ readonly isUpdateAvailable?: boolean; readonly updateInfo?: { readonly version?: string } } | null>;
  downloadUpdate(): Promise<unknown>;
  quitAndInstall(): void;
}

export interface ElectronUpdaterOptions {
  readonly client: Client;
  /** DPAppRegistry's identity for the app. version defaults to app.getVersion(), platform and arch to process.platform and process.arch. */
  readonly check: Omit<CheckOptions, 'version'> & { readonly version?: string };
  /** Shown in the dialogs. */
  readonly productName: string;
  readonly electron: ElectronModules;
  /** electron-updater's autoUpdater. Without it every update is a download. */
  readonly autoUpdater?: AutoUpdaterLike;
  /** Whether this install can replace itself. Default: Windows, a Linux AppImage, or a Developer ID signed macOS app, since Squirrel.Mac verifies the new bundle's signature. */
  readonly canInstallInPlace?: () => boolean | Promise<boolean>;
  /** Download formats, most preferred first (DPAppRegistry package names: file extensions without the dot). */
  readonly packages?: readonly string[];
  readonly firstCheckMs?: number;
  readonly intervalMs?: number;
  readonly log?: (message: string, err?: unknown) => void;
}

export interface ElectronUpdater {
  /** Runs background checks: firstCheckMs after the call, then every intervalMs. */
  start(): void;
  stop(): void;
  /** A check the user asked for: also reports "up to date" and failures, and re-offers a declined update. */
  checkNow(): Promise<void>;
}

const DEFAULT_PACKAGES: Readonly<Record<string, readonly string[]>> = {
  darwin: ['dmg', 'zip', 'pkg'],
  win32: ['exe', 'msi', 'zip'],
  linux: ['AppImage', 'deb', 'rpm'],
};

/**
 * The folder an electron-builder latest*.yml URL lives in: electron-updater's
 * generic feed. Null when the URL names no folder, such as a /download?key=…
 * link, since the feed's installers are resolved relative to it.
 */
export function feedFolder(ymlUrl: string): string | null {
  let u: URL;
  try {
    u = new URL(ymlUrl);
  } catch {
    return null;
  }
  if (!/\.ya?ml$/i.test(u.pathname)) return null;
  u.pathname = u.pathname.replace(/[^/]*$/, '');
  u.search = '';
  u.hash = '';
  return u.href;
}

async function defaultCanInstallInPlace(app: ElectronModules['app']): Promise<boolean> {
  if (!app.isPackaged) return false;
  if (process.platform === 'win32') return true;
  if (process.platform === 'linux') return !!process.env.APPIMAGE;
  if (process.platform !== 'darwin') return false;
  const bundle = path.resolve(process.execPath, '..', '..', '..');
  return new Promise((resolve) => {
    execFile('codesign', ['-dv', '--verbose=2', bundle], (_err, _stdout, stderr) => {
      resolve(/Authority=Developer ID Application/.test(String(stderr)));
    });
  });
}

export function createElectronUpdater(opts: ElectronUpdaterOptions): ElectronUpdater {
  const { client, productName, electron, autoUpdater } = opts;
  const { app, dialog, shell } = electron;
  const log = opts.log ?? ((message: string, err?: unknown) => console.error(`[updater] ${message}`, err ?? ''));
  const packages = opts.packages ?? DEFAULT_PACKAGES[systemPlatform()] ?? [];
  const canInstallInPlace = opts.canInstallInPlace ?? (() => defaultCanInstallInPlace(app));
  let checking = false;
  let dismissed: string | null = null;
  let timers: ReturnType<typeof setTimeout>[] = [];

  if (autoUpdater) {
    autoUpdater.autoDownload = false;
    autoUpdater.autoInstallOnAppQuit = true;
  }

  const ask = async (message: string, detail: string, yes: string): Promise<boolean> => {
    const { response } = await dialog.showMessageBox({ type: 'info', buttons: [yes, 'Later'], defaultId: 0, cancelId: 1, message, detail });
    return response === 0;
  };
  const tell = (message: string, detail: string, failed = false): Promise<unknown> =>
    dialog.showMessageBox({ type: failed ? 'error' : 'info', message, detail });

  const describe = (resp: UpdateResponse, version: string, rest: string): string => {
    const notes = resp.changelog.trim();
    return `${resp.critical ? 'This update is marked critical. ' : ''}You have ${version}. ${rest}${notes ? `\n\n${notes}` : ''}`;
  };

  // In place through electron-updater; false when the feed turned out to offer
  // nothing newer, so the caller can report that.
  const installInPlace = async (feed: string, resp: UpdateResponse, version: string, interactive: boolean): Promise<boolean> => {
    autoUpdater!.setFeedURL({ provider: 'generic', url: feed });
    const result = await autoUpdater!.checkForUpdates();
    if (!result?.isUpdateAvailable) return false;
    if (!interactive && !resp.critical && dismissed === feed) return true;
    const next = result.updateInfo?.version ? `${productName} ${result.updateInfo.version}` : `A new version of ${productName}`;
    if (!(await ask(`${next} is available`, describe(resp, version, 'It downloads now and installs when you restart.'), 'Download and Install'))) {
      dismissed = feed;
      return true;
    }
    await autoUpdater!.downloadUpdate();
    if (await ask(`${next} is ready to install`, 'Restart now to finish updating, or it installs the next time you quit.', 'Restart Now')) {
      autoUpdater!.quitAndInstall();
    }
    return true;
  };

  const check = async (interactive: boolean): Promise<void> => {
    if (checking) return;
    checking = true;
    const version = opts.check.version ?? app.getVersion();
    try {
      const resp = await client.checkForUpdates({
        ...opts.check,
        version,
        platform: opts.check.platform ?? systemPlatform(),
        arch: opts.check.arch ?? systemArch(),
      });
      if (!resp.updateAvailable) {
        if (interactive) await tell(`${productName} is up to date`, `Version ${version} is the newest available.`);
        return;
      }
      const yml = resp.packageUrls.find((p) => p.package.toLowerCase() === 'yml');
      const feed = yml ? feedFolder(yml.url) : null;
      if (feed && autoUpdater && (await canInstallInPlace())) {
        if (await installInPlace(feed, resp, version, interactive)) return;
        if (interactive) await tell(`${productName} is up to date`, `Version ${version} is the newest available.`);
        return;
      }
      const download = packages
        .map((want) => resp.packageUrls.find((p) => p.package.toLowerCase() === want.toLowerCase())?.url)
        .find((url): url is string => !!url) ?? (resp.updateUrl || null);
      if (!download) {
        log(`an update is available, but not as a download for ${systemPlatform()}`);
        if (interactive) await tell('Update not available here', `A new version of ${productName} exists, but not as a download for this system.`, true);
        return;
      }
      if (!interactive && !resp.critical && dismissed === download) return;
      if (await ask(`A new version of ${productName} is available`, describe(resp, version, 'The installer downloads in your browser.'), 'Download')) {
        await shell.openExternal(download);
      } else {
        dismissed = download;
      }
    } catch (err) {
      log('update check failed', err);
      if (interactive) await tell('Could not check for updates', err instanceof Error ? err.message : String(err), true);
    } finally {
      checking = false;
    }
  };

  const stop = (): void => {
    for (const t of timers) clearTimeout(t);
    timers = [];
  };
  const start = (): void => {
    stop();
    const first = setTimeout(() => {
      void check(false);
      const every = setInterval(() => void check(false), opts.intervalMs ?? 6 * 60 * 60 * 1000);
      every.unref?.();
      timers.push(every);
    }, opts.firstCheckMs ?? 30_000);
    first.unref?.();
    timers = [first];
  };
  return { start, stop, checkNow: () => check(true) };
}
