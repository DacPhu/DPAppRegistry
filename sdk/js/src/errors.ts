import type { EndpointSource } from './types';

export class DPAppRegistryError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'DPAppRegistryError';
  }
}

export class ValidationError extends DPAppRegistryError {
  constructor(message: string) {
    super(message);
    this.name = 'ValidationError';
  }
}

export class RequestFailedError extends DPAppRegistryError {
  constructor(message: string) {
    super(message);
    this.name = 'RequestFailedError';
  }
}

export class EndpointError extends RequestFailedError {
  constructor(
    public readonly source: EndpointSource,
    public readonly endpointUrl: string,
    public readonly statusCode?: number,
    cause?: Error,
  ) {
    let msg: string;
    if (statusCode !== undefined) {
      msg = `dpappregistry: request failed: ${endpointUrl} returned HTTP ${statusCode}`;
    } else if (cause !== undefined) {
      msg = `dpappregistry: request failed: ${endpointUrl}: ${cause.message}`;
    } else {
      msg = `dpappregistry: request failed: ${endpointUrl}`;
    }
    super(msg);
    this.name = 'EndpointError';
    if (cause !== undefined) {
      this.cause = cause;
    }
  }
}

export class CheckError extends RequestFailedError {
  constructor(
    public readonly edgeError?: Error,
    public readonly apiError?: Error,
  ) {
    let msg: string;
    if (edgeError !== undefined && apiError !== undefined) {
      msg = `dpappregistry: request failed: edge failed: ${edgeError.message}; api failed: ${apiError.message}`;
    } else if (edgeError !== undefined) {
      msg = `dpappregistry: request failed: edge failed: ${edgeError.message}`;
    } else if (apiError !== undefined) {
      msg = `dpappregistry: request failed: api failed: ${apiError.message}`;
    } else {
      msg = 'dpappregistry: request failed';
    }
    super(msg);
    this.name = 'CheckError';
  }
}

export class UnsupportedUpdaterError extends ValidationError {
  constructor(public readonly updater: string) {
    super(`dpappregistry: unsupported updater: ${updater}`);
    this.name = 'UnsupportedUpdaterError';
  }
}

export const ErrMissingBaseURL = new ValidationError('dpappregistry: missing base URL');
export const ErrInvalidBaseURL = new ValidationError('dpappregistry: invalid base URL');
export const ErrInvalidEdgeURL = new ValidationError('dpappregistry: invalid edge URL');
export const ErrMissingOwner = new ValidationError('dpappregistry: missing owner');
export const ErrMissingAppName = new ValidationError('dpappregistry: missing app name');
export const ErrMissingVersion = new ValidationError('dpappregistry: missing version');
export const ErrMissingReportKey = new ValidationError('dpappregistry: missing report key');
export const ErrMissingDeviceId = new ValidationError('dpappregistry: missing device id');
export const ErrMissingChannel = new ValidationError('dpappregistry: missing channel');
export const ErrMissingPlatform = new ValidationError('dpappregistry: missing platform');
export const ErrMissingArch = new ValidationError('dpappregistry: missing arch');
export const ErrInvalidEventType = new ValidationError('dpappregistry: invalid event type');
export const ErrInvalidReason = new ValidationError('dpappregistry: invalid event reason');
