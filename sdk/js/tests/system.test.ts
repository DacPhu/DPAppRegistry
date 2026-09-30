import { systemArch, systemPlatform } from '../src/system';

describe('systemPlatform', () => {
  it('returns process.platform', () => {
    expect(systemPlatform()).toBe(process.platform);
  });

  it('returns a non-empty string', () => {
    expect(typeof systemPlatform()).toBe('string');
    expect(systemPlatform().length).toBeGreaterThan(0);
  });
});

describe('systemArch', () => {
  it('returns process.arch', () => {
    expect(systemArch()).toBe(process.arch);
  });

  it('returns a non-empty string', () => {
    expect(typeof systemArch()).toBe('string');
    expect(systemArch().length).toBeGreaterThan(0);
  });
});
