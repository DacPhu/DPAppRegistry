import { evaluateRollout, rolloutBucket } from '../src/rollout';

const SEED = 'badadc23b08e3943';

describe('rolloutBucket', () => {
  it('is deterministic for the same device and seed', () => {
    expect(rolloutBucket('device-1', SEED)).toBe(rolloutBucket('device-1', SEED));
  });

  it('matches the reference algorithm', () => {
    expect(rolloutBucket('device-1', SEED)).toBe(18);
    expect(rolloutBucket('device-2', SEED)).toBe(22);
    expect(rolloutBucket('device-abc', SEED)).toBe(5);
  });

  it('always falls within [0, 99]', () => {
    for (let i = 0; i < 500; i++) {
      const bucket = rolloutBucket(`device-${i}`, SEED);
      expect(bucket).toBeGreaterThanOrEqual(0);
      expect(bucket).toBeLessThan(100);
    }
  });
});

describe('evaluateRollout', () => {
  it('includes the install when its bucket is below the percent', () => {
    const r = evaluateRollout(20, SEED, 'device-1'); // bucket 18
    expect(r).toEqual({ percent: 20, seed: SEED, bucket: 18, eligible: true });
  });

  it('excludes the install when its bucket is at or above the percent', () => {
    const r = evaluateRollout(20, SEED, 'device-2'); // bucket 22
    expect(r).toEqual({ percent: 20, seed: SEED, bucket: 22, eligible: false });
  });

  it('is sticky when the percent is raised', () => {
    expect(evaluateRollout(20, SEED, 'device-1').eligible).toBe(true);
    expect(evaluateRollout(50, SEED, 'device-1').eligible).toBe(true);
  });

  it('excludes when no deviceId is supplied', () => {
    expect(evaluateRollout(20, SEED, undefined)).toEqual({
      percent: 20,
      seed: SEED,
      bucket: null,
      eligible: false,
    });
    expect(evaluateRollout(20, SEED, '')).toEqual({
      percent: 20,
      seed: SEED,
      bucket: null,
      eligible: false,
    });
  });
});
