import React, { useEffect, useState } from 'react';
import axiosInstance from '@/config/axios';

// The account's plan next to what it uses (GET /account/plan). Added to the
// upstream dashboard by DPAppRegistry's deploy (deploy/dashboard-patch.cjs).

interface Limits {
  apps: number;
  storage_bytes: number;
  members: number;
  mai: number;
}

interface AppInstalls {
  app: string;
  measured: number;
  estimated: number;
}

interface Overview {
  owner: string;
  plan: { id: string; name: string; features: string[] | null; hard_mai_cap: boolean; overage_usd?: number; overage_vnd?: number };
  limits: Limits;
  enabled: boolean;
  usage: {
    apps: number;
    members: number;
    storage_bytes: number;
    installs: { month: string; measured: number; estimated: number; apps: AppInstalls[] };
  };
  store_url?: string;
  contact?: string;
}

const FEATURES: { id: string; label: string }[] = [
  { id: 'private_apps', label: 'Private apps' },
  { id: 'rollouts', label: 'Staged rollouts and required intermediate versions' },
  { id: 'statistics', label: 'Install statistics' },
  { id: 'reports', label: 'Crash reports' },
  { id: 'tuf', label: 'TUF signing' },
  { id: 'store_branding', label: 'Store branding' },
];

const formatBytes = (bytes: number): string => {
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unit]}`;
};

const formatCount = (n: number): string => n.toLocaleString('en-US');

interface MeterProps {
  label: string;
  used: number;
  limit: number;
  format?: (n: number) => string;
  note?: string;
}

const Meter: React.FC<MeterProps> = ({ label, used, limit, format = formatCount, note }) => {
  const percent = limit > 0 ? Math.min(100, (used / limit) * 100) : 0;
  const state = limit > 0 && used >= limit ? 'full' : limit > 0 && used >= 0.8 * limit ? 'near' : 'ok';
  const bar = state === 'full' ? 'bg-red-500' : state === 'near' ? 'bg-amber-400' : 'bg-theme-button-submit';
  return (
    <div className="rounded-lg p-4 border border-theme-card-hover">
      <div className="mb-2 flex items-baseline justify-between gap-3">
        <span className="text-sm font-semibold text-theme-primary">{label}</span>
        <span className="text-sm text-theme-primary opacity-80">
          {format(used)} of {limit > 0 ? format(limit) : 'unlimited'}
        </span>
      </div>
      <div
        className="h-2 w-full overflow-hidden rounded-full bg-white/10"
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(percent)}
      >
        <div className={`h-full rounded-full ${bar}`} style={{ width: `${percent}%` }} />
      </div>
      {state !== 'ok' && (
        <p className={`mt-2 text-xs font-semibold ${state === 'full' ? 'text-red-300' : 'text-amber-300'}`}>
          {state === 'full' ? 'Limit reached' : 'More than 80% used'}
        </p>
      )}
      {note && <p className="mt-2 text-xs text-theme-primary opacity-70">{note}</p>}
    </div>
  );
};

export const PlanSettings: React.FC = () => {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    axiosInstance
      .get<Overview>('/account/plan')
      .then((response) => setOverview(response.data))
      .catch((err) => setError(err?.response?.data?.error || err?.message || 'The plan could not be loaded.'));
  }, []);

  if (error) {
    return <div className="rounded-lg p-4 border border-red-500/40 text-red-200">{error}</div>;
  }
  if (!overview) {
    return (
      <div className="text-theme-primary opacity-80">
        <i className="fas fa-spinner fa-spin mr-2"></i>Loading the plan…
      </div>
    );
  }

  const { plan, limits, usage } = overview;
  const features = plan.features ?? [];
  const installs = usage.installs;
  const installsNote = [
    `${formatCount(installs.measured)} reported by the apps, ${formatCount(installs.estimated)} estimated from checks without an install ID.`,
    plan.hard_mai_cap
      ? 'Past the limit, updates keep working but new uploads stop.'
      : plan.overage_usd
        ? `Past the limit, each started 1,000 installs costs $${plan.overage_usd} (${formatCount(plan.overage_vnd ?? 0)} ₫).`
        : '',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div className="space-y-6">
      <div className="rounded-lg p-4 border border-theme-card-hover">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <p className="text-xs font-semibold uppercase tracking-wider text-theme-primary opacity-70">Plan</p>
            <h2 className="text-2xl font-bold text-theme-primary">{plan.name}</h2>
            <p className="mt-1 text-sm text-theme-primary opacity-80">
              Account {overview.owner}
              {overview.enabled ? '' : ' · limits are not enforced on this server'}
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            {overview.store_url && (
              <a
                href={overview.store_url}
                target="_blank"
                rel="noopener noreferrer"
                className="bg-theme-button-primary text-theme-primary px-4 py-2 rounded-lg hover:bg-theme-button-primary-hover transition-colors duration-200"
              >
                <i className="fas fa-store mr-2"></i>Store page
              </a>
            )}
            {overview.contact && (
              <a
                href={`mailto:${overview.contact}?subject=${encodeURIComponent(`Plan for ${overview.owner}`)}`}
                className="rounded-lg border border-white/25 px-4 py-2 font-semibold text-theme-primary transition-colors hover:bg-white/10"
              >
                <i className="fas fa-envelope mr-2"></i>Change plan
              </a>
            )}
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Meter label={`Active installs in ${installs.month}`} used={installs.measured + installs.estimated} limit={limits.mai} note={installsNote} />
        <Meter label="Storage" used={usage.storage_bytes} limit={limits.storage_bytes} format={formatBytes} />
        <Meter label="Apps" used={usage.apps} limit={limits.apps} />
        <Meter label="Members" used={usage.members} limit={limits.members} note="The account itself and its team users." />
      </div>

      {installs.apps.length > 0 && (
        <div className="rounded-lg p-4 border border-theme-card-hover">
          <h3 className="mb-3 text-md font-semibold text-theme-primary">Active installs by app</h3>
          <table className="w-full text-sm text-theme-primary">
            <thead>
              <tr className="text-left opacity-70">
                <th className="py-1 font-semibold">App</th>
                <th className="py-1 text-right font-semibold">Reported</th>
                <th className="py-1 text-right font-semibold">Estimated</th>
              </tr>
            </thead>
            <tbody>
              {installs.apps.map((app) => (
                <tr key={app.app} className="border-t border-white/10">
                  <td className="py-1">{app.app}</td>
                  <td className="py-1 text-right">{formatCount(app.measured)}</td>
                  <td className="py-1 text-right">{formatCount(app.estimated)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="rounded-lg p-4 border border-theme-card-hover">
        <h3 className="mb-3 text-md font-semibold text-theme-primary">Included in {plan.name}</h3>
        <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          {FEATURES.map((feature) => {
            const included = features.includes(feature.id);
            return (
              <li key={feature.id} className={`text-sm text-theme-primary ${included ? '' : 'opacity-50'}`}>
                <i className={`fas ${included ? 'fa-check text-green-400' : 'fa-times'} mr-2 w-4`}></i>
                {feature.label}
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
};
