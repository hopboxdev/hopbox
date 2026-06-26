// Package box is hopbox's standalone compute-box core — the primitive a user
// reaches with `ssh box@host`. It owns the box request grammar, backend
// selection, lifetime/flavor, and (incrementally) the box model + engine.
//
// Boundary rule: box MUST NOT import the dev-environment layer (core/workspace,
// api, gateway, identity). The dependency points only inward — workspace and the
// dev-env build on top of box — so the box product can be compiled and shipped
// without any of them. Keep `go list -deps ./internal/core/box` free of those
// packages.
package box
