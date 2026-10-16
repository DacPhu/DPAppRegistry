// Changes the upstream dashboard before deploy.sh builds it, inside the build
// container, so the upstream source is never changed:
//
// - it can run under a sub-path (vite --base=/registry/ beside the store page):
//   its routes, its sign-out redirect and its sidebar logo use the base instead of "/";
// - the tab shows this project's name;
// - an upload shows its progress (bytes sent, speed, time left) and can be
//   cancelled while the files are still going up;
// - Settings has a Plan tab: the account's plan, limits and usage
//   (dashboard/PlanSettings.tsx, from GET /account/plan).
//
// Each change must match the upstream code exactly once, and each added file must
// be new to upstream; if upstream moved any of it, the build stops here instead of
// shipping a half-patched dashboard.

const fs = require('fs');
const path = require('path');

const addedFiles = {
  'src/components/settings/PlanSettings.tsx': path.join(__dirname, 'dashboard', 'PlanSettings.tsx'),
};

const uploadProgress = `
// Upload progress, added by DPAppRegistry's deploy: bytes sent, speed and time
// left while the files go up, then the server's turn to store them.
const formatBytes = (bytes: number): string => {
  if (bytes < 1024) return \`\${bytes} B\`;
  const units = ['KB', 'MB', 'GB'];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return \`\${value.toFixed(value < 10 ? 1 : 0)} \${units[unit]}\`;
};

const formatDuration = (seconds: number): string => {
  const s = Math.max(0, Math.round(seconds));
  if (s < 60) return \`\${s}s\`;
  const m = Math.floor(s / 60);
  return m < 60 ? \`\${m}m \${s % 60}s\` : \`\${Math.floor(m / 60)}h \${m % 60}m\`;
};

type UploadProgressState = { loaded: number; total: number; startedAt: number; canceled?: boolean };

const UploadProgress: React.FC<UploadProgressState> = ({ loaded, total, startedAt, canceled }) => {
  const percent = total > 0 ? Math.min(100, Math.floor((loaded / total) * 100)) : 0;
  const sent = total > 0 && loaded >= total;
  const elapsed = (Date.now() - startedAt) / 1000;
  const speed = elapsed > 0 ? loaded / elapsed : 0;
  let detail = \`\${formatBytes(loaded)} of \${formatBytes(total)}\`;
  if (speed > 0) detail += \` · \${formatBytes(speed)}/s · \${formatDuration((total - loaded) / speed)} left\`;
  if (sent) detail = 'All files sent. The server is storing them…';
  if (canceled) detail = 'Upload cancelled before the server got every file, so nothing was saved.';
  return (
    <div className="mb-4 rounded-lg border border-white/15 bg-white/5 p-3" role="status" aria-live="polite">
      <div className="mb-2 flex items-center justify-between text-xs font-semibold text-white/70">
        <span>{canceled ? 'Cancelled' : sent ? 'Processing' : 'Uploading'}</span>
        <span>{percent}%</span>
      </div>
      <div
        className="h-2 w-full overflow-hidden rounded-full bg-white/10"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
      >
        <div
          className={\`h-full rounded-full transition-all duration-200 \${canceled ? 'bg-red-500' : 'bg-theme-button-submit'} \${sent && !canceled ? 'animate-pulse' : ''}\`}
          style={{ width: \`\${percent}%\` }}
        />
      </div>
      <p className="mt-2 font-mono text-[11.5px] text-white/60">{detail}</p>
    </div>
  );
};
`;

const edits = [
  ['src/route/router.tsx', /\]\);\s*$/, "], { basename: import.meta.env.BASE_URL.replace(/\\/$/, '') || '/' });\n"],
  ['src/config/axios.ts', "window.location.href = '/signin';", "window.location.href = import.meta.env.BASE_URL + 'signin';"],
  ['src/components/layout/Sidebar.tsx', /src="\/banner-small\.png"/g, "src={import.meta.env.BASE_URL + 'banner-small.png'}"],
  ['index.html', '<title>FaynoSync</title>', '<title>DPAppRegistry</title>'],

  // The upload request reports progress and can be aborted.
  ['src/hooks/use-query/useUploadQuery.ts', "import axiosInstance from '@/config/axios';", "import axiosInstance from '@/config/axios';\nimport type { AxiosProgressEvent } from 'axios';"],
  [
    'src/hooks/use-query/useUploadQuery.ts',
    '  signature?: string;\n};',
    '  signature?: string;\n  onUploadProgress?: (event: AxiosProgressEvent) => void;\n  signal?: AbortSignal;\n};',
  ],
  [
    'src/hooks/use-query/useUploadQuery.ts',
    "const response = await axiosInstance.post('/upload', formData, {",
    "const response = await axiosInstance.post('/upload', formData, {\n        onUploadProgress: data.onUploadProgress,\n        signal: data.signal,",
  ],

  // The modal shows what it is given above its buttons, and its Cancel button
  // can stop the work in progress instead of staying disabled.
  [
    'src/components/common/AdvancedModal.tsx',
    '  files?: FileInfo[];\n}',
    '  files?: FileInfo[];\n  progress?: React.ReactNode;\n  onCancelLoading?: () => void;\n  cancelLoadingText?: string;\n}',
  ],
  [
    'src/components/common/AdvancedModal.tsx',
    '  files = [],\n}) => {',
    "  files = [],\n  progress,\n  onCancelLoading,\n  cancelLoadingText = 'Cancel',\n}) => {",
  ],
  [
    'src/components/common/AdvancedModal.tsx',
    '          <div className="flex justify-end gap-2">\n            <button type="button" onClick={onClose} disabled={isLoading} className={BTN_GHOST}>\n              Cancel\n            </button>',
    '          {progress}\n          <div className="flex justify-end gap-2">\n            <button\n              type="button"\n              onClick={isLoading && onCancelLoading ? onCancelLoading : onClose}\n              disabled={isLoading && !onCancelLoading}\n              className={BTN_GHOST}\n            >\n              {isLoading && onCancelLoading ? cancelLoadingText : \'Cancel\'}\n            </button>',
  ],

  // The upload form tracks the bytes sent and holds the request's abort switch.
  [
    'src/components/modals/UploadModal.tsx',
    '  const [uploadError, setUploadError] = useState<string | null>(null);',
    '  const [uploadError, setUploadError] = useState<string | null>(null);\n  const [progress, setProgress] = useState<UploadProgressState | null>(null);\n  const abortRef = React.useRef<AbortController | null>(null);\n  React.useEffect(() => () => abortRef.current?.abort(), []);',
  ],
  [
    'src/components/modals/UploadModal.tsx',
    '      await upload(uploadData);',
    '      const controller = new AbortController();\n      abortRef.current = controller;\n      const startedAt = Date.now();\n      setProgress({ loaded: 0, total: files.reduce((sum, f) => sum + f.file.size, 0), startedAt });\n      await upload({\n        ...uploadData,\n        signal: controller.signal,\n        onUploadProgress: (event) =>\n          setProgress((current) => ({ loaded: event.loaded, total: event.total || current?.total || 0, startedAt })),\n      });',
  ],
  [
    'src/components/modals/UploadModal.tsx',
    "    } catch (error: any) {\n      console.error('Upload failed:', error);",
    "    } catch (error: any) {\n      if (error?.code === 'ERR_CANCELED') {\n        setProgress((current) => (current ? { ...current, canceled: true } : current));\n        return;\n      }\n      setProgress(null);\n      console.error('Upload failed:', error);",
  ],
  [
    'src/components/modals/UploadModal.tsx',
    '      isLoading={isLoading}\n',
    "      isLoading={isLoading}\n      progress={progress && <UploadProgress {...progress} />}\n      onCancelLoading={\n        progress && !progress.canceled && progress.loaded < progress.total ? () => abortRef.current?.abort() : undefined\n      }\n      cancelLoadingText=\"Cancel upload\"\n",
  ],
  ['src/components/modals/UploadModal.tsx', /\s*$/, `\n${uploadProgress}`],

  // The Plan tab in Settings, at /settings/plan.
  [
    'src/pages/SettingsPage.tsx',
    "import { TokenSettings } from '@/components/settings/TokenSettings';",
    "import { TokenSettings } from '@/components/settings/TokenSettings';\nimport { PlanSettings } from '@/components/settings/PlanSettings';",
  ],
  ['src/pages/SettingsPage.tsx', "type SettingsPage = 'users' | 'tokens' | 'tuf';", "type SettingsPage = 'users' | 'tokens' | 'tuf' | 'plan';"],
  [
    'src/pages/SettingsPage.tsx',
    "    if (location.pathname === '/settings/tuf') {",
    "    if (location.pathname === '/settings/plan') {\n      return 'plan';\n    }\n    if (location.pathname === '/settings/tuf') {",
  ],
  [
    'src/pages/SettingsPage.tsx',
    "    { id: 'tuf' as SettingsPage, label: 'TUF', icon: 'fa-shield-alt' },",
    "    { id: 'tuf' as SettingsPage, label: 'TUF', icon: 'fa-shield-alt' },\n    { id: 'plan' as SettingsPage, label: 'Plan', icon: 'fa-chart-pie' },",
  ],
  [
    'src/pages/SettingsPage.tsx',
    "    if (page === 'tuf') {\n      navigate('/settings/tuf');",
    "    if (page === 'plan') {\n      navigate('/settings/plan');\n    } else if (page === 'tuf') {\n      navigate('/settings/tuf');",
  ],
  [
    'src/pages/SettingsPage.tsx',
    "                {currentPage === 'tuf' && <TufSettings />}",
    "                {currentPage === 'tuf' && <TufSettings />}\n                {currentPage === 'plan' && <PlanSettings />}",
  ],
  [
    'src/route/router.tsx',
    "    path: '/settings/tokens',\n    element: <RootLayout><PrivateRoute><SettingsPage /></PrivateRoute></RootLayout>,\n  },",
    "    path: '/settings/tokens',\n    element: <RootLayout><PrivateRoute><SettingsPage /></PrivateRoute></RootLayout>,\n  },\n  {\n    path: '/settings/plan',\n    element: <RootLayout><PrivateRoute><SettingsPage /></PrivateRoute></RootLayout>,\n  },",
  ],
];

for (const [file, source] of Object.entries(addedFiles)) {
  if (fs.existsSync(file)) {
    console.error(`dashboard-patch: upstream now has ${file}; rename ours in deploy/dashboard-patch.cjs`);
    process.exit(1);
  }
  fs.copyFileSync(source, file);
}

for (const [file, from, to] of edits) {
  const before = fs.readFileSync(file, 'utf8');
  const found = typeof from === 'string' ? before.split(from).length - 1 : from.test(before) ? 1 : 0;
  if (found !== 1) {
    console.error(`dashboard-patch: ${file} has the expected code ${found} times, not once; update deploy/dashboard-patch.cjs`);
    process.exit(1);
  }
  fs.writeFileSync(file, before.replace(from, to));
}
