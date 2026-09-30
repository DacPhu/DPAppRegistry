# TUF Client Example (DPAppRegistry Adaptation)

This directory contains a local adaptation of the upstream `go-tuf` client example.

## What was adapted for DPAppRegistry

- `metadataURL` points to DPAppRegistry metadata storage.
- `targetsURL` points to DPAppRegistry target storage.
- `targetName` is set to a DPAppRegistry target path.
- `cfg.PrefixTargetsWithHash` is set to `false` to match DPAppRegistry target URL layout.

## License note

The source file `client.go` is based on code from The Update Framework `go-tuf` examples and keeps its original Apache-2.0 license header and SPDX identifier.
