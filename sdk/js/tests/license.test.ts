import { createPrivateKey, generateKeyPairSync, sign } from 'node:crypto';
import { ErrLicenseFormat, ErrLicenseSignature, ErrNoLicensePublicKey } from '../src/errors';
import { licenseAllowsCluster, licenseCoversUpdate, licenseExpired, licenseKeyId, parseLicense } from '../src/license';

// The Go SDK's vector (sdk/go/licensekey/licensekey_test.go): seed 0x01 × 32
// and this payload give exactly this key, so keys issued by one implementation
// read in the other.
const VECTOR_KEY =
  'dpl1.eyJpZCI6IjY3MDdhMWIyYzNkNGU1ZjZhN2I4YzlkMCIsInByb2R1Y3QiOiJkcGZzIiwicGxhbiI6ImJ1c2luZXNzIiwiY3VzdG9tZXIiOnsibmFtZSI6IkPDtG5nIHR5IEFCQyIsImVtYWlsIjoiaXRAYWJjLnZuIn0sImNsdXN0ZXJzIjoyLCJpc3N1ZWRfYXQiOiIyMDI2LTEwLTA5VDAzOjAwOjAwWiIsImV4cGlyZXNfYXQiOiIyMDI3LTEwLTA5VDAzOjAwOjAwWiIsInVwZGF0ZXNfdW50aWwiOiIyMDI3LTEwLTA5VDAzOjAwOjAwWiIsImlzc3VlciI6InN0b3JlLm1pdGVrYnJvLmNvbSIsImtpZCI6IjM0NzUwZjk4In0.F0zG2CTLBOll3JjahBtWt6iTn-lSS9HQj4AikNSU57eUTPtlZcCjtMMuN2hJ7ZwIBg-rm2QeHYJWycrhgWCqDw';
const VECTOR_PUBLIC_KEY = '8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c';
const VECTOR_SEED = Buffer.alloc(32, 1);

// Wraps a raw Ed25519 seed as PKCS#8, which node:crypto reads.
function privateKeyFromSeed(seed: Buffer) {
  return createPrivateKey({ key: Buffer.concat([Buffer.from('302e020100300506032b657004220420', 'hex'), seed]), format: 'der', type: 'pkcs8' });
}

function signKey(payload: object, privateKey: ReturnType<typeof privateKeyFromSeed>): string {
  const segment = Buffer.from(JSON.stringify(payload)).toString('base64url');
  const signature = sign(null, Buffer.from(`dpl1.${segment}`, 'ascii'), privateKey).toString('base64url');
  return `dpl1.${segment}.${signature}`;
}

describe('parseLicense', () => {
  it('reads the Go SDK vector', () => {
    const license = parseLicense(VECTOR_KEY, [VECTOR_PUBLIC_KEY]);
    expect(license.id).toBe('6707a1b2c3d4e5f6a7b8c9d0');
    expect(license.plan).toBe('business');
    expect(license.customer).toEqual({ name: 'Công ty ABC', email: 'it@abc.vn' });
    expect(license.clusters).toBe(2);
    expect(license.clusterIds).toEqual([]);
    expect(license.expiresAt.toISOString()).toBe('2027-10-09T03:00:00.000Z');
    expect(license.updatesUntil?.toISOString()).toBe('2027-10-09T03:00:00.000Z');
    expect(license.keyId).toBe('34750f98');
    expect(licenseKeyId(VECTOR_PUBLIC_KEY)).toBe('34750f98');
  });

  it('signs the vector payload to the same bytes as Go', () => {
    const [, payload] = VECTOR_KEY.split('.');
    const signature = sign(null, Buffer.from(`dpl1.${payload}`, 'ascii'), privateKeyFromSeed(VECTOR_SEED)).toString('base64url');
    expect(`dpl1.${payload}.${signature}`).toBe(VECTOR_KEY);
  });

  it('ignores whitespace a mail client added', () => {
    const wrapped = VECTOR_KEY.replace(/(.{60})/g, '$1\r\n ');
    expect(parseLicense(`\t${wrapped}\n`, [VECTOR_PUBLIC_KEY]).id).toBe('6707a1b2c3d4e5f6a7b8c9d0');
  });

  it('accepts raw bytes and several keys, the right one anywhere', () => {
    const { publicKey: otherPublic } = generateKeyPairSync('ed25519');
    const other = otherPublic.export({ format: 'der', type: 'spki' }).subarray(-32);
    expect(parseLicense(VECTOR_KEY, [other, Buffer.from(VECTOR_PUBLIC_KEY, 'hex')]).plan).toBe('business');
  });

  it('refuses tampering, wrong keys and non-keys', () => {
    const [prefix, payload, signature] = VECTOR_KEY.split('.');
    expect(() => parseLicense(`${prefix}.${payload.slice(0, -2)}AA.${signature}`, [VECTOR_PUBLIC_KEY])).toThrow(ErrLicenseSignature);
    expect(() => parseLicense(`${prefix}.${payload}.${signature.slice(0, -2)}AA`, [VECTOR_PUBLIC_KEY])).toThrow(ErrLicenseSignature);
    const { publicKey } = generateKeyPairSync('ed25519');
    const otherKey = publicKey.export({ format: 'der', type: 'spki' }).subarray(-32).toString('hex');
    expect(() => parseLicense(VECTOR_KEY, [otherKey])).toThrow(ErrLicenseSignature);
    expect(() => parseLicense(VECTOR_KEY, [])).toThrow(ErrNoLicensePublicKey);
    for (const bad of ['', 'dpl2.a.b', `${prefix}.${payload}`, `${prefix}.${payload}.not base64!`]) {
      expect(() => parseLicense(bad, [VECTOR_PUBLIC_KEY])).toThrow(ErrLicenseFormat);
    }
  });

  it('wants the essentials in the payload', () => {
    const { privateKey, publicKey } = generateKeyPairSync('ed25519');
    const pub = publicKey.export({ format: 'der', type: 'spki' }).subarray(-32);
    const key = signKey({ id: 'x', product: 'dpfs', plan: 'business', issued_at: '2026-10-09T00:00:00Z' }, privateKey as never);
    expect(() => parseLicense(key, [pub])).toThrow(ErrLicenseFormat);
  });
});

describe('license checks', () => {
  const license = parseLicense(VECTOR_KEY, [VECTOR_PUBLIC_KEY]);
  const before = new Date('2027-10-08T00:00:00Z');
  const after = new Date('2027-10-10T00:00:00Z');

  it('expiry, update window and clusters', () => {
    expect(licenseExpired(license, before)).toBe(false);
    expect(licenseExpired(license, after)).toBe(true);
    expect(licenseCoversUpdate(license, before)).toBe(true);
    expect(licenseCoversUpdate(license, after)).toBe(false);
    expect(licenseCoversUpdate(license, undefined)).toBe(true);
    expect(licenseCoversUpdate({ ...license, updatesUntil: undefined }, after)).toBe(true);
    expect(licenseAllowsCluster(license, 'anything')).toBe(true);
    const locked = { ...license, clusterIds: ['aaa'] };
    expect(licenseAllowsCluster(locked, 'aaa')).toBe(true);
    expect(licenseAllowsCluster(locked, 'bbb')).toBe(false);
  });
});
