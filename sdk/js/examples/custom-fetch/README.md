# custom-fetch

Update check with a custom `fetch` function and explicit timeout.

Demonstrates:

- Supplying a custom `fetch` to `Client` for connection reuse via `https.Agent`
- Setting `timeoutMs` to control per-request deadline
- Passing `deviceId` alongside a custom transport

## When to use a custom `fetch`

The default `globalThis.fetch` (Node.js 18+ built-in) creates a new connection for every request. For applications that poll frequently, supply a custom `fetch` backed by an agent with `keepAlive: true` to reuse connections and reduce latency.

```ts
const client = new Client({
  baseURL: 'https://api.example.com',
  fetch: myFetch,   // e.g. wraps undici or node-fetch with a shared agent
  timeoutMs: 10_000,
});
```

## Run

```sh
npx ts-node examples/custom-fetch/index.ts
```

Expects a DPAppRegistry server running at `http://localhost:9000`.
