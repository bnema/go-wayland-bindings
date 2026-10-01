// Package bindings is the root of github.com/bnema/go-wayland-bindings, Go
// bindings for the Wayland protocols vendored in spec/.
//
// Client bindings live in client/<pkg> and server bindings in server/<pkg>.
// The root package has no API; it only carries the go:generate directive that
// regenerates every binding from spec/manifest.json.
package bindings

//go:generate go run ./cmd/wlbgen -all
