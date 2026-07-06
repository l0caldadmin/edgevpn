# Dependency Refresh Notes

Status: **Complete** · Phase: **Milestone 5**

## What Changed
- Ran a patch-level dependency refresh across the module graph.
- Applied safe updates to direct and indirect dependencies.
- Re-pinned the Pion WebRTC stack to the last known-good versions after a compile-time incompatibility surfaced during validation.
- Ran `go mod tidy` to normalize `go.mod` and `go.sum`.

## Notable Updates
- `github.com/labstack/echo/v4` -> `v4.15.4`
- `dario.cat/mergo` -> `v1.0.2`
- `github.com/ipfs/go-datastore` -> `v0.9.2`
- `github.com/spf13/cast` -> `v1.7.1`
- `github.com/xrash/smetrics` -> latest patch pseudoversion
- Several indirect `golang.org/x/*`, `go.opentelemetry.io/*`, and `github.com/pion/*` modules were refreshed and then stabilized as needed.

## Validation
- `go test -vet=off ./pkg/config ./pkg/blockchain ./pkg/node`
- `go test -vet=off ./...`

## Notes
- The dependency refresh is intentionally patch-level only.
- No API surface changes are expected from this milestone.
- If future patch updates introduce incompatibilities, prefer pinning the affected module family rather than widening to a major upgrade.
