# basic

Minimal update check using the DPAppRegistry JS SDK.

Demonstrates:

- Creating a `Client` with only `baseURL`
- Using `systemPlatform()` and `systemArch()` to pass Node.js runtime values
- Distinguishing request failures (`RequestFailedError`, the base class of `CheckError`/`EndpointError`) from validation errors

## Run

```sh
npx ts-node examples/basic/index.ts
```

Expects a DPAppRegistry server running at `http://localhost:9000`.
