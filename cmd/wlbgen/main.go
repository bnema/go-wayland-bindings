// Command wlbgen generates Go bindings from Wayland protocol XML.
//
// Usage:
//
//	wlbgen -all [-root DIR]
//	wlbgen -side client|server -package NAME -out FILE [-import iface=path ...] protocol.xml
//
// With -all, wlbgen renders client/<pkg>/<pkg>_generated.go and
// server/<pkg>/<pkg>_generated.go below DIR for every protocol listed in the
// embedded spec manifest, and removes generated files of packages that left
// the manifest.
//
// With -side, wlbgen generates a single file from a private protocol XML.
// Interfaces defined in other protocols are resolved from the embedded
// manifest (import path .../client/<owner> or .../server/<owner>); each
// -import iface=path overrides that resolution for one interface. An
// interface that cannot be resolved is an error.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bnema/go-wayland-bindings/internal/gen/client"
	"github.com/bnema/go-wayland-bindings/internal/gen/server"
	"github.com/bnema/go-wayland-bindings/spec"
)

const tool = "wlbgen"

type importFlags map[string]string

func (f importFlags) String() string { return fmt.Sprint(map[string]string(f)) }

func (f importFlags) Set(v string) error {
	iface, p, ok := strings.Cut(v, "=")
	if !ok || iface == "" || p == "" {
		return fmt.Errorf("want iface=importpath, got %q", v)
	}
	f[iface] = p
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "wlbgen:", err)
		os.Exit(1)
	}
}

func run(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet(tool, flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("all", false, "generate client and server bindings for every manifest protocol")
	root := fs.String("root", ".", "repository root (with -all)")
	side := fs.String("side", "", "`client` or `server` (single file mode)")
	pkg := fs.String("package", "", "Go package name (single file mode)")
	out := fs.String("out", "", "output file (single file mode)")
	imports := importFlags{}
	fs.Var(imports, "import", "`iface=importpath` override for a foreign interface (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	m, err := spec.Load()
	if err != nil {
		return err
	}
	if *all {
		if fs.NArg() != 0 || *side != "" || *pkg != "" || *out != "" || len(imports) != 0 {
			return fmt.Errorf("-all takes no other arguments except -root")
		}
		return generateAll(m, m.Open, *root)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("want -all, or -side/-package/-out and one XML file")
	}
	if *pkg == "" || *out == "" {
		return fmt.Errorf("-package and -out are required")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	src, err := generateCustom(m, *side, *pkg, filepath.Base(fs.Arg(0)), data, imports)
	if err != nil {
		return err
	}
	return writeFile(*out, src)
}

func writeFile(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o644)
}

func clientImport(owner string) string { return spec.ModulePath + "/client/" + owner }
func serverImport(owner string) string { return spec.ModulePath + "/server/" + owner }

func generatedPath(root, side, pkg string) string {
	return filepath.Join(root, side, pkg, pkg+"_generated.go")
}

// generated is one rendered output file.
type generated struct {
	path string
	data []byte
}

// generateAll writes every package of m below root and removes stale output.
// Everything is rendered in memory first, so an error leaves the tree as it
// was.
func generateAll(m *spec.Manifest, open func(string) ([]byte, error), root string) error {
	owners := m.Owners()
	var files []generated
	for _, e := range m.Protocols {
		data, err := open(e.Package)
		if err != nil {
			return err
		}
		p, err := spec.Parse(data)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Package, err)
		}

		s := client.NewScanner()
		s.Tool = tool
		spdx := ""
		if e.License != "" && e.License != "MIT" {
			spdx = e.License
		}
		s.SPDXLicense = spdx
		s.CrossPackage = map[string]string{}
		for iface, owner := range owners {
			if owner != e.Package {
				s.CrossPackage[iface] = clientImport(owner)
			}
		}
		if err := s.LoadProtocol(p, path.Base(e.Path)); err != nil {
			return err
		}
		src, err := s.Generate(e.Package)
		if err != nil {
			return fmt.Errorf("client/%s: %w", e.Package, err)
		}
		files = append(files, generated{generatedPath(root, "client", e.Package), src})

		opts := server.Options{
			Package: e.Package, Imports: map[string]string{}, Owners: map[string]string{}, Tool: tool,
			Copyright: p.Copyright, SPDX: spdx,
		}
		for iface, owner := range owners {
			if owner != e.Package {
				opts.Owners[iface] = owner
			}
		}
		for _, dep := range e.Deps {
			if dep != e.Package {
				opts.Imports[dep] = serverImport(dep)
			}
		}
		src, err = server.Generate(p, opts)
		if err != nil {
			return fmt.Errorf("server/%s: %w", e.Package, err)
		}
		files = append(files, generated{generatedPath(root, "server", e.Package), src})
	}
	if err := removeStale(m, root); err != nil {
		return err
	}
	for _, f := range files {
		if err := writeFile(f.path, f.data); err != nil {
			return err
		}
	}
	return nil
}

// removeStale deletes <side>/<pkg>/<pkg>_generated.go for packages that are
// not in m. If such a directory holds anything else, nothing is deleted and an
// error lists the remaining files.
func removeStale(m *spec.Manifest, root string) error {
	known := map[string]bool{}
	for _, e := range m.Protocols {
		known[e.Package] = true
	}
	type stale struct{ dir, gen string }
	var list []stale
	var problems []string
	for _, side := range []string{"client", "server"} {
		entries, err := os.ReadDir(filepath.Join(root, side))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, d := range entries {
			if !d.IsDir() || known[d.Name()] {
				continue
			}
			dir := filepath.Join(root, side, d.Name())
			gen := filepath.Join(dir, d.Name()+"_generated.go")
			if _, err := os.Stat(gen); err != nil {
				continue // not generated output; leave alone
			}
			files, err := os.ReadDir(dir)
			if err != nil {
				return err
			}
			for _, f := range files {
				if f.Name() != d.Name()+"_generated.go" {
					problems = append(problems, filepath.Join(side, d.Name(), f.Name()))
				}
			}
			list = append(list, stale{dir, gen})
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("package removed from manifest but directory still holds other files: %s", strings.Join(problems, ", "))
	}
	for _, s := range list {
		if err := os.Remove(s.gen); err != nil {
			return err
		}
		if err := os.Remove(s.dir); err != nil {
			return err
		}
	}
	return nil
}

// foreignInterfaces returns, sorted, the interfaces referenced by object and
// new_id arguments of p that p does not define.
func foreignInterfaces(p *spec.Protocol) []string {
	local := map[string]bool{}
	for _, it := range p.Interfaces {
		local[it.Name] = true
	}
	seen := map[string]bool{}
	for _, it := range p.Interfaces {
		for _, msgs := range [][]spec.Message{it.Requests, it.Events} {
			for _, msg := range msgs {
				for _, a := range msg.Args {
					if (a.Type == "object" || a.Type == "new_id") && a.Interface != "" && !local[a.Interface] {
						seen[a.Interface] = true
					}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func generateCustom(m *spec.Manifest, side, pkg, source string, data []byte, overrides map[string]string) ([]byte, error) {
	p, err := spec.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	foreign := foreignInterfaces(p)
	owners := m.Owners()
	switch side {
	case "client":
		s := client.NewScanner()
		s.Tool = tool
		s.CrossPackage = map[string]string{}
		for _, iface := range foreign {
			if p, ok := overrides[iface]; ok {
				s.CrossPackage[iface] = p
			} else if owner, ok := owners[iface]; ok {
				s.CrossPackage[iface] = clientImport(owner)
			}
			// Unresolved interfaces are reported by the generator unless
			// they are transport bootstrap types.
		}
		if err := s.LoadProtocol(p, source); err != nil {
			return nil, err
		}
		return s.Generate(pkg)
	case "server":
		opts := server.Options{
			Package: pkg, Imports: map[string]string{}, Owners: map[string]string{}, Tool: tool,
			Copyright: p.Copyright,
		}
		for _, iface := range foreign {
			var alias, importPath string
			if p, ok := overrides[iface]; ok {
				alias, importPath = importAlias(p), p
			} else if owner, ok := owners[iface]; ok {
				alias, importPath = owner, serverImport(owner)
			} else {
				return nil, fmt.Errorf("unresolved interface %q: not in the manifest; add -import %s=<importpath>", iface, iface)
			}
			if prev, ok := opts.Imports[alias]; ok && prev != importPath {
				return nil, fmt.Errorf("import alias %q is used for both %q and %q", alias, prev, importPath)
			}
			opts.Imports[alias] = importPath
			opts.Owners[iface] = alias
		}
		return server.Generate(p, opts)
	default:
		return nil, fmt.Errorf("-side must be client or server, got %q", side)
	}
}

// importAlias derives a Go identifier from the last element of an import path.
func importAlias(p string) string {
	b := []byte(path.Base(p))
	for i, c := range b {
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
			b[i] = '_'
		}
	}
	return string(b)
}
