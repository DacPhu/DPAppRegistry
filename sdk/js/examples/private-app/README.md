# private-app

Update check and artifact download for a private app using the DPAppRegistry JS SDK.

Demonstrates:

- Passing `downloadToken` for a private app in `strict` mode (read from `DPAPPREGISTRY_DOWNLOAD_TOKEN`)
- Downloading the artifact from the `/download` link with the same `X-Download-Token` header (`DOWNLOAD_TOKEN_HEADER`)
- Following the redirect to presigned storage by hand (`redirect: 'manual'`), so the token is not sent to the storage host

## Run

```sh
DPAPPREGISTRY_DOWNLOAD_TOKEN=fnd_... npx ts-node examples/private-app/index.ts
```

Expects a DPAppRegistry server running at `http://localhost:9000` with a private app `internal-tool` owned by `admin`, and a download token for its `stable` channel (`POST /download-tokens/regenerate`). Without the token the example works for a public or `unlisted` app; a `strict` app answers as if it did not exist.
