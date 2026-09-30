import type { ValidationError } from './errors';

export function parseAbsoluteURL(raw: string, sentinel: ValidationError): URL {
  let u: URL;
  try {
    u = new URL(raw);
  } catch {
    throw sentinel;
  }
  if (!u.protocol || !u.hostname) {
    throw sentinel;
  }
  return u;
}

export function joinURLPath(basePath: string, segment: string): string {
  if (basePath === '' || basePath === '/') {
    return `/${segment}`;
  }
  return `${basePath.replace(/\/+$/, '')}/${segment}`;
}
