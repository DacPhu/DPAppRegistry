# CI/CD

Better and more polished CI/CD examples live in [faynoSync-cli](https://github.com/ku9nov/faynoSync-cli) (upstream):

- [GitHub Actions](https://github.com/ku9nov/faynoSync-cli/blob/main/examples/github-actions/upload.yml) — uses the `ku9nov/faynoSync-cli@v1` composite action.
- [Jenkins](https://github.com/ku9nov/faynoSync-cli/tree/main/examples/jenkins) — a shared-library `dpappregistryUpload` step.

Both authenticate with a scoped API token (`DPAPPREGISTRY_TOKEN`) instead of a username and password.
