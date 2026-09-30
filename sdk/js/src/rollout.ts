import { createHash } from 'node:crypto';
import type { RolloutInfo } from './types';

// Deterministic bucket in [0, 99]: sha256(deviceId + ":" + seed), first 8 bytes as a
// big-endian uint64, modulo 100. This is the reference algorithm every DPAppRegistry SDK
// must replicate byte-for-byte so a device's rollout decision matches across SDKs.
export function rolloutBucket(deviceId: string, seed: string): number {
  const digest = createHash('sha256').update(`${deviceId}:${seed}`).digest();
  return Number(digest.readBigUInt64BE(0) % 100n);
}

// Decides whether this install is inside a staged rollout. Without a deviceId the bucket
// cannot be computed, so the install stays out of the canary until one is provided.
export function evaluateRollout(
  percent: number,
  seed: string,
  deviceId: string | undefined,
): RolloutInfo {
  if (deviceId === undefined || deviceId === '') {
    return { percent, seed, bucket: null, eligible: false };
  }
  const bucket = rolloutBucket(deviceId, seed);
  return { percent, seed, bucket, eligible: bucket < percent };
}
