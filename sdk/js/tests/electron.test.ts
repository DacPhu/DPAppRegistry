import { Client } from '../src/client';
import { createElectronUpdater, feedFolder } from '../src/electron';
import type { AutoUpdaterLike, ElectronModules, ElectronUpdaterOptions } from '../src/electron';
import { createTestServer, writeJSON } from './helpers';
import type { TestServer } from './helpers';

interface Shown {
  message: string;
  detail?: string;
  buttons?: string[];
}

// Answers each dialog from `answers` (0 = the first button), then "Later".
function fakeElectron(answers: number[] = []) {
  const shown: Shown[] = [];
  const opened: string[] = [];
  const electron: ElectronModules = {
    app: { getVersion: () => '1.0.0', isPackaged: true },
    dialog: {
      showMessageBox: async (o) => {
        shown.push(o);
        return { response: answers.shift() ?? 1 };
      },
    },
    shell: {
      openExternal: async (url) => {
        opened.push(url);
      },
    },
  };
  return { electron, shown, opened };
}

function fakeAutoUpdater(isUpdateAvailable = true) {
  const calls: string[] = [];
  const au: AutoUpdaterLike & { feed?: string } = {
    autoDownload: true,
    autoInstallOnAppQuit: false,
    setFeedURL(o) {
      au.feed = o.url;
    },
    async checkForUpdates() {
      return { isUpdateAvailable, updateInfo: { version: '1.2.0' } };
    },
    async downloadUpdate() {
      calls.push('download');
    },
    quitAndInstall() {
      calls.push('install');
    },
  };
  return { au, calls };
}

const FEED = 'https://cdn.example.com/electron-builder/app-admin/1.2.0/stable/darwin/arm64/';
let server: TestServer;
let status = 200;
let body: unknown;

beforeEach(async () => {
  status = 200;
  server = await createTestServer((req, res) => {
    if (!req.url?.startsWith('/checkVersion')) {
      res.statusCode = 404;
      res.end();
    } else if (status !== 200) {
      res.statusCode = status;
      res.end('oops');
    } else {
      writeJSON(res, body);
    }
  });
});

afterEach(() => server.close());

function updater(electron: ElectronModules, extra: Partial<ElectronUpdaterOptions> = {}) {
  return createElectronUpdater({
    client: new Client({ baseURL: server.url }),
    check: { owner: 'admin', appName: 'app', channel: 'stable', platform: 'darwin', arch: 'arm64' },
    productName: 'App',
    electron,
    packages: ['dmg', 'zip'],
    canInstallInPlace: () => true,
    log: () => {},
    ...extra,
  });
}

const settle = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe('feedFolder', () => {
  it('turns a latest*.yml URL into its folder', () => {
    expect(feedFolder(`${FEED}latest-mac.yml?v=1`)).toBe(FEED);
  });
  it('rejects URLs that name no folder', () => {
    expect(feedFolder('https://api.example.com/download?key=a%2Flatest-mac.yml')).toBeNull();
    expect(feedFolder('not a url')).toBeNull();
  });
});

describe('createElectronUpdater', () => {
  it('opens the preferred download when there is no feed to install from', async () => {
    body = { update_available: true, update_url_zip: 'https://r/app.zip', update_url_dmg: 'https://r/app.dmg' };
    const { electron, opened } = fakeElectron([0]);
    await updater(electron).checkNow();
    expect(opened).toEqual(['https://r/app.dmg']);
  });

  it('hands an electron-builder feed to electron-updater and installs on restart', async () => {
    body = { update_available: true, update_url_yml: `${FEED}latest-mac.yml`, update_url_zip: `${FEED}app.zip` };
    const { electron, shown, opened } = fakeElectron([0, 0]);
    const { au, calls } = fakeAutoUpdater();
    await updater(electron, { autoUpdater: au }).checkNow();
    expect(au.feed).toBe(FEED);
    expect(au.autoDownload).toBe(false);
    expect(calls).toEqual(['download', 'install']);
    expect(shown[0].message).toBe('App 1.2.0 is available');
    expect(opened).toEqual([]);
  });

  it('downloads instead when this install cannot replace itself', async () => {
    body = { update_available: true, update_url_yml: `${FEED}latest-mac.yml`, update_url_zip: `${FEED}app.zip` };
    const { electron, opened } = fakeElectron([0]);
    const { au, calls } = fakeAutoUpdater();
    await updater(electron, { autoUpdater: au, canInstallInPlace: () => false }).checkNow();
    expect(au.feed).toBeUndefined();
    expect(calls).toEqual([]);
    expect(opened).toEqual([`${FEED}app.zip`]);
  });

  it('downloads instead when the feed URL names no folder', async () => {
    body = { update_available: true, update_url_yml: 'https://api/download?key=x%2Flatest-mac.yml', update_url_dmg: 'https://r/app.dmg' };
    const { electron, opened } = fakeElectron([0]);
    await updater(electron, { autoUpdater: fakeAutoUpdater().au }).checkNow();
    expect(opened).toEqual(['https://r/app.dmg']);
  });

  it('reports up to date and failures only when asked', async () => {
    body = { update_available: false };
    const quiet = fakeElectron();
    const background = updater(quiet.electron, { firstCheckMs: 1 });
    background.start();
    await settle(50);
    background.stop();
    expect(quiet.shown).toEqual([]);

    const asked = fakeElectron();
    await updater(asked.electron).checkNow();
    status = 500;
    await updater(asked.electron).checkNow();
    expect(asked.shown.map((s) => s.message)).toEqual(['App is up to date', 'Could not check for updates']);
  });

  it('does not re-offer a declined update in the background, but does when asked', async () => {
    body = { update_available: true, update_url_dmg: 'https://r/app.dmg' };
    const { electron, shown } = fakeElectron();
    const u = updater(electron, { firstCheckMs: 1, intervalMs: 10 });
    u.start();
    await settle(80);
    u.stop();
    expect(shown).toHaveLength(1);
    await u.checkNow();
    expect(shown).toHaveLength(2);
  });

  it('re-offers a critical update on every check', async () => {
    body = { update_available: true, critical: true, update_url_dmg: 'https://r/app.dmg' };
    const { electron, shown } = fakeElectron();
    const u = updater(electron, { firstCheckMs: 1, intervalMs: 10 });
    u.start();
    await settle(80);
    u.stop();
    expect(shown.length).toBeGreaterThanOrEqual(2);
    expect(shown[0].detail).toMatch(/^This update is marked critical\./);
  });
});
