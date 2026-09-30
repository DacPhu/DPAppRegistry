import {
  CheckError,
  EndpointError,
  DPAppRegistryError,
  RequestFailedError,
  ValidationError,
} from '../src/errors';

describe('EndpointError', () => {
  it('formats message with statusCode', () => {
    const err = new EndpointError('api', 'http://api.example.com/checkVersion', 404);
    expect(err.message).toBe(
      'dpappregistry: request failed: http://api.example.com/checkVersion returned HTTP 404',
    );
    expect(err.statusCode).toBe(404);
    expect(err.source).toBe('api');
    expect(err.endpointUrl).toBe('http://api.example.com/checkVersion');
  });

  it('formats message with cause when no statusCode', () => {
    const cause = new Error('connection refused');
    const err = new EndpointError('edge', 'http://cdn.example.com/path', undefined, cause);
    expect(err.message).toBe(
      'dpappregistry: request failed: http://cdn.example.com/path: connection refused',
    );
    expect(err.cause).toBe(cause);
    expect(err.statusCode).toBeUndefined();
  });

  it('formats message with neither statusCode nor cause', () => {
    const err = new EndpointError('edge', 'http://cdn.example.com/path');
    expect(err.message).toBe('dpappregistry: request failed: http://cdn.example.com/path');
    expect(err.statusCode).toBeUndefined();
    expect(err.cause).toBeUndefined();
  });

  it('is instanceof RequestFailedError and DPAppRegistryError', () => {
    const err = new EndpointError('api', 'http://example.com', 500);
    expect(err).toBeInstanceOf(EndpointError);
    expect(err).toBeInstanceOf(RequestFailedError);
    expect(err).toBeInstanceOf(DPAppRegistryError);
    expect(err).toBeInstanceOf(Error);
  });
});

describe('CheckError', () => {
  it('formats message with both edgeError and apiError', () => {
    const edgeErr = new Error('edge failed');
    const apiErr = new Error('api failed');
    const err = new CheckError(edgeErr, apiErr);
    expect(err.message).toBe(
      'dpappregistry: request failed: edge failed: edge failed; api failed: api failed',
    );
    expect(err.edgeError).toBe(edgeErr);
    expect(err.apiError).toBe(apiErr);
  });

  it('formats message with only edgeError', () => {
    const edgeErr = new Error('edge failed');
    const err = new CheckError(edgeErr);
    expect(err.message).toBe('dpappregistry: request failed: edge failed: edge failed');
    expect(err.edgeError).toBe(edgeErr);
    expect(err.apiError).toBeUndefined();
  });

  it('formats message with only apiError', () => {
    const apiErr = new Error('api failed');
    const err = new CheckError(undefined, apiErr);
    expect(err.message).toBe('dpappregistry: request failed: api failed: api failed');
    expect(err.edgeError).toBeUndefined();
    expect(err.apiError).toBe(apiErr);
  });

  it('formats message with neither error', () => {
    const err = new CheckError();
    expect(err.message).toBe('dpappregistry: request failed');
    expect(err.edgeError).toBeUndefined();
    expect(err.apiError).toBeUndefined();
  });

  it('is instanceof RequestFailedError and DPAppRegistryError', () => {
    const err = new CheckError();
    expect(err).toBeInstanceOf(CheckError);
    expect(err).toBeInstanceOf(RequestFailedError);
    expect(err).toBeInstanceOf(DPAppRegistryError);
    expect(err).toBeInstanceOf(Error);
  });
});

describe('ValidationError', () => {
  it('is instanceof DPAppRegistryError but not RequestFailedError', () => {
    const err = new ValidationError('missing field');
    expect(err).toBeInstanceOf(ValidationError);
    expect(err).toBeInstanceOf(DPAppRegistryError);
    expect(err).not.toBeInstanceOf(RequestFailedError);
  });
});
