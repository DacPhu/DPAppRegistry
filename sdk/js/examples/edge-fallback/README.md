# edge-fallback

Update check with a static edge CDN configured alongside the base API.

Demonstrates:

- Configuring `edgeURL` for a static JSON edge response
- Automatic fallback to `baseURL` when the edge misses or fails
- Passing `deviceId` to trigger a telemetry beacon after a successful edge hit
- Reading `resp.source` to see which endpoint served the response

## How it works

1. The SDK requests `GET {edgeURL}/responses/{owner}/{appName}/{channel}/{platform}/{arch}/{version}.json`.
2. On HTTP 200 with valid JSON, the response is returned with `source: 'edge'` and a telemetry beacon is sent to `{baseURL}/telemetry/beacon`.
3. On any failure (404, network error, invalid JSON), the SDK retries against `{baseURL}/checkVersion`.

## Run

```sh
npx ts-node examples/edge-fallback/index.ts
```

Expects a DPAppRegistry server at `http://localhost:9000` and an edge server at `http://cb-dpappregistry-s3-public.web.garage.localhost:3902`.
