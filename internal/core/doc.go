// Package core holds Mesa's vendor-neutral domain: the Workspace resource,
// provider ports (Go interfaces), the state store interface, and the reconciler.
//
// ARCHITECTURAL RULE: no code under internal/core may import a provider SDK
// (docker, sqlite driver, yamux, pty, cloud SDKs). Core sees only its own
// ports and neutral types. This is what keeps the seams real (see plan task 13).
package core
