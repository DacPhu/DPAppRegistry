// License keys: a signed statement of what a customer bought, which an app
// verifies offline with the publisher's public key. The format is the Go SDK's
// (sdk/go/licensekey): "dpl1.<payload>.<signature>", the payload being the
// base64url JSON of the license and the signature Ed25519 over the ASCII bytes
// "dpl1." + payload. Whitespace anywhere is ignored: mail clients wrap keys.

import { createHash, createPublicKey, verify as verifySignature } from 'node:crypto';
import { ErrLicenseFormat, ErrLicenseSignature, ErrNoLicensePublicKey, LicenseError } from './errors';
import type { License } from './types';

export const LICENSE_PREFIX = 'dpl1';

// The DER prefix that turns a raw Ed25519 public key into SubjectPublicKeyInfo.
const SPKI_ED25519 = Buffer.from('302a300506032b6570032100', 'hex');

function rawPublicKey(key: string | Uint8Array): Buffer {
  const raw = typeof key === 'string' ? Buffer.from(key.trim(), 'hex') : Buffer.from(key);
  if (raw.length !== 32) throw new LicenseError('dpappregistry: an Ed25519 public key is 32 bytes (64 hex digits)');
  return raw;
}

/** Names a public key the way licenses do (their `kid`): the first 8 hex digits of its SHA-256. */
export function licenseKeyId(publicKey: string | Uint8Array): string {
  return createHash('sha256').update(rawPublicKey(publicKey)).digest('hex').slice(0, 8);
}

/**
 * Verifies a license key against the publisher's public keys (raw 32 bytes,
 * or hex) and returns the license. Throws ErrLicenseFormat for a string that
 * is not a key, ErrLicenseSignature when no key verifies it.
 */
export function parseLicense(key: string, publicKeys: ReadonlyArray<string | Uint8Array>): License {
  if (publicKeys.length === 0) throw ErrNoLicensePublicKey;
  const parts = key.replace(/\s+/g, '').split('.');
  if (parts.length !== 3 || parts[0] !== LICENSE_PREFIX || !parts[1] || !parts[2]) throw ErrLicenseFormat;
  if (!/^[A-Za-z0-9_-]+$/.test(parts[1]) || !/^[A-Za-z0-9_-]+$/.test(parts[2])) throw ErrLicenseFormat;
  const signature = Buffer.from(parts[2], 'base64url');
  if (signature.length !== 64) throw ErrLicenseFormat;
  const message = Buffer.from(`${LICENSE_PREFIX}.${parts[1]}`, 'ascii');

  const verified = publicKeys.some((candidate) => {
    const spki = createPublicKey({ key: Buffer.concat([SPKI_ED25519, rawPublicKey(candidate)]), format: 'der', type: 'spki' });
    return verifySignature(null, message, spki, signature);
  });
  if (!verified) throw ErrLicenseSignature;

  let payload: Record<string, unknown>;
  try {
    payload = JSON.parse(Buffer.from(parts[1], 'base64url').toString('utf8')) as Record<string, unknown>;
  } catch {
    throw ErrLicenseFormat;
  }
  const text = (field: string): string => (typeof payload[field] === 'string' ? (payload[field] as string) : '');
  const date = (field: string): Date | undefined => {
    const value = text(field);
    if (!value) return undefined;
    const parsed = new Date(value);
    return Number.isNaN(parsed.getTime()) ? undefined : parsed;
  };
  const customer = (payload.customer ?? {}) as Record<string, unknown>;
  const issuedAt = date('issued_at');
  const expiresAt = date('expires_at');
  if (!text('id') || !text('product') || !text('plan') || !issuedAt || !expiresAt) throw ErrLicenseFormat;
  return {
    id: text('id'),
    product: text('product'),
    plan: text('plan'),
    customer: {
      name: typeof customer.name === 'string' ? customer.name : '',
      email: typeof customer.email === 'string' ? customer.email : '',
    },
    clusters: typeof payload.clusters === 'number' ? payload.clusters : 0,
    clusterIds: Array.isArray(payload.cluster_ids) ? payload.cluster_ids.filter((id): id is string => typeof id === 'string') : [],
    issuedAt,
    expiresAt,
    updatesUntil: date('updates_until'),
    issuer: text('issuer'),
    keyId: text('kid'),
  };
}

/** Whether the license term ended before now. */
export function licenseExpired(license: License, now: Date = new Date()): boolean {
  return now.getTime() > license.expiresAt.getTime();
}

/**
 * Whether the license entitles its holder to a release published at the given
 * time. A release with no date, or a license with no updates window, is covered.
 */
export function licenseCoversUpdate(license: License, publishedAt?: Date): boolean {
  if (!publishedAt || !license.updatesUntil) return true;
  return publishedAt.getTime() <= license.updatesUntil.getTime();
}

/** Whether the license may be used on this cluster; one naming no clusters may be used on any. */
export function licenseAllowsCluster(license: License, clusterId: string): boolean {
  return license.clusterIds.length === 0 || license.clusterIds.includes(clusterId);
}
