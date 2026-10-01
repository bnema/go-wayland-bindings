# go-wayland-bindings

Go bindings for the Wayland protocols, generated from vendored upstream XML.
One module is the single source of truth for both sides of the wire:

- `client/<pkg>`: client bindings for the pure-Go
  [wlturbo](https://github.com/bnema/wlturbo) runtime.
- `server/<pkg>`: server bindings for the libwayland-server runtime in
  [purego-libwayland](https://github.com/bnema/purego-libwayland).

Everything builds with `CGO_ENABLED=0`.

## Install

```sh
go get github.com/bnema/go-wayland-bindings@latest
```

## Usage

Client (the runtime lives in `github.com/bnema/wlturbo`):

```go
import (
	"github.com/bnema/wlturbo/wl"

	"github.com/bnema/go-wayland-bindings/client/wayland"
)

display, err := wl.Connect("")
// ...
compositor := wayland.NewCompositor(display.Context())
err = display.Registry().Bind(name, wayland.CompositorInterface, 1, compositor)
```

Server (the runtime lives in `github.com/bnema/purego-libwayland/server`):

```go
import (
	"github.com/bnema/purego-libwayland/server"

	"github.com/bnema/go-wayland-bindings/server/wayland"
)

err := wayland.NewCompositorGlobal(display, 6, func(c server.Client, version, id uint32) {
	_, _ = wayland.NewCompositor(c, int32(version), id, myCompositorHandler{})
})
```

## Packages

A package name derives from the XML file name: drop `.xml`, drop a trailing
`-unstable-vN`, then a trailing `-vN`, then remove every `-`. So
`xdg-shell.xml` is `xdgshell`, `wlr-layer-shell-unstable-v1.xml` is
`wlrlayershell`, and `wayland.xml` is `wayland`. The package has the same name
under `client/` and `server/`. When several upstream versions of one protocol
exist, the most stable and then highest version is used; there is one major
version per protocol.

The full list, with upstream source, path, license and interfaces, is in
[`spec/manifest.json`](spec/manifest.json).

## Custom protocol XML

`wlbgen` generates bindings for a private protocol XML. Interfaces it
references from the vendored protocols (such as `wl_surface`) are resolved
automatically to the matching `client/<pkg>` or `server/<pkg>` package:

```sh
go run github.com/bnema/go-wayland-bindings/cmd/wlbgen \
    -side server -package mything -out mything_generated.go mything.xml
```

Use `-side client` for client bindings. `-import iface=importpath` overrides
the package of one interface (repeatable); an interface that cannot be
resolved is an error.

## Development

Regenerate every binding from `spec/manifest.json`:

```sh
go generate ./...
```

Generated output is deterministic: a second run changes nothing. Hand-written
files that live next to generated code (for example
`client/linuxdmabuf/table.go`) are never touched.

Sync the vendored XML with upstream (needs network and `git`):

```sh
go run ./cmd/wlbsync            # update spec/xml and spec/manifest.json
go run ./cmd/wlbsync -check     # print changed=true|false, write nothing
go run ./cmd/wlbsync -notes notes.md
```

`spec/manifest.json` is written by `wlbsync` and is never edited by hand.

## Releases

A scheduled workflow syncs upstream every day. When protocol XML changed it
regenerates the bindings, runs the checks and publishes a new minor version
`v0.X.0` whose release notes list added and removed packages, interface
version bumps and breaking changes. A moved upstream ref with identical XML
publishes nothing, and neither does a failed run.

## Tests

```sh
env CGO_ENABLED=0 go test ./...
go test -race ./...          # needs libwayland-server installed
```

The end-to-end tests in `internal/e2e` (and the race run) need
`libwayland-server.so.0`, which the server runtime loads at run time. Without
it those tests are skipped, with the reason shown by `go test -v`; any other
server start failure still fails them.

Two environment variables enable tests that are skipped by default:

- `WLTURBO_HEADLESS`: path to a headless compositor binary (NeferWL) for the
  end-to-end tests in `client/`.
- `WLTURBO_LIVE`: set to `1` to run the tests that talk to the compositor of
  the current session.

## Licenses

The code of this repository (generators, tooling, tests) is MIT licensed,
copyright go-wayland-bindings contributors; see [`LICENSE`](LICENSE).

The vendored XML and the bindings generated from it keep the license of the
upstream protocol; each generated file carries the upstream copyright notice.
Most protocols are MIT. The `serverdecoration` protocol (KDE
`server-decoration.xml`) is LGPL-2.1-or-later; its text is in
[`LICENSES/`](LICENSES/LGPL-2.1-or-later.txt).
