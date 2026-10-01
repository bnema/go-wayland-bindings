package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bnema/go-wayland-bindings/spec"
)

// candidate is an upstream XML file that may provide a package.
type candidate struct {
	source    string
	path      string // slash-separated path inside the repository
	pkg       string
	stability Stability
	major     int
	data      []byte
}

// tree is the desired content of spec/: the manifest plus the XML files.
type tree struct {
	manifest *spec.Manifest
	xml      map[string][]byte // file name under spec/xml
}

// rank orders candidates of one package: higher is better.
func (c candidate) beats(o candidate) bool {
	if c.stability != o.stability {
		return c.stability > o.stability
	}
	return c.major > o.major
}

func (c candidate) tied(o candidate) bool {
	return c.stability == o.stability && c.major == o.major
}

// collect resolves and checks out every source and returns the candidates
// found, together with the resolved source records.
func collect(srcs []upstream, pins map[string]string, f Fetcher) ([]spec.Source, []candidate, error) {
	var (
		sources []spec.Source
		cands   []candidate
	)
	for _, src := range srcs {
		ref, commit, err := f.Resolve(src, pins[src.ID])
		if err != nil {
			return nil, nil, fmt.Errorf("resolve %s: %w", src.ID, err)
		}
		found, err := checkoutAndScan(src, ref, commit, f)
		if err != nil {
			return nil, nil, err
		}
		if len(found) == 0 {
			return nil, nil, fmt.Errorf("%s@%s: no protocol XML found", src.ID, ref)
		}
		sources = append(sources, spec.Source{ID: src.ID, URL: src.URL, Ref: ref, Commit: commit})
		cands = append(cands, found...)
	}
	return sources, cands, nil
}

func checkoutAndScan(src upstream, ref, commit string, f Fetcher) ([]candidate, error) {
	dir, err := os.MkdirTemp("", "wlbsync-"+src.ID+"-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := f.Checkout(src, ref, commit, dir); err != nil {
		return nil, fmt.Errorf("checkout %s@%s: %w", src.ID, ref, err)
	}
	var out []candidate
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // symlinks and other special files are never vendored
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		st, ok := src.Match(rel)
		if !ok {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out = append(out, candidate{
			source:    src.ID,
			path:      rel,
			pkg:       spec.PackageName(rel),
			stability: st,
			major:     fileMajor(rel),
			data:      data,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", src.ID, err)
	}
	return out, nil
}

// build applies the selection rule and derives the manifest.
func build(sources []spec.Source, cands []candidate) (*tree, error) {
	byPkg := map[string][]candidate{}
	for _, c := range cands {
		if !spec.ValidPackageName(c.pkg) {
			return nil, fmt.Errorf("%s:%s maps to invalid package name %q", c.source, c.path, c.pkg)
		}
		byPkg[c.pkg] = append(byPkg[c.pkg], c)
	}

	t := &tree{manifest: &spec.Manifest{Sources: sources}, xml: map[string][]byte{}}
	parsed := map[string]*spec.Protocol{}
	pkgs := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	for _, pkg := range pkgs {
		cs := byPkg[pkg]
		sort.Slice(cs, func(i, j int) bool {
			if cs[i].beats(cs[j]) != cs[j].beats(cs[i]) {
				return cs[i].beats(cs[j])
			}
			return cs[i].source+":"+cs[i].path < cs[j].source+":"+cs[j].path
		})
		best := cs[0]
		if len(cs) > 1 && best.tied(cs[1]) {
			return nil, fmt.Errorf("package %q: %s:%s and %s:%s tie (same stability and major version)",
				pkg, best.source, best.path, cs[1].source, cs[1].path)
		}
		p, err := spec.Parse(best.data)
		if err != nil {
			return nil, fmt.Errorf("%s:%s: %w", best.source, best.path, err)
		}
		if strings.TrimSpace(p.Copyright) == "" {
			return nil, fmt.Errorf("%s:%s: missing <copyright>", best.source, best.path)
		}
		parsed[pkg] = p

		sum := sha256.Sum256(best.data)
		entry := spec.ProtocolEntry{
			Package: pkg,
			XML:     pkg + ".xml",
			Source:  best.source,
			Path:    best.path,
			SHA256:  hex.EncodeToString(sum[:]),
			License: licenseFor(pkg),
			Deps:    []string{},
		}
		for _, it := range p.Interfaces {
			entry.Interfaces = append(entry.Interfaces, spec.InterfaceEntry{Name: it.Name, Version: it.Version})
		}
		sort.Slice(entry.Interfaces, func(i, j int) bool { return entry.Interfaces[i].Name < entry.Interfaces[j].Name })
		t.manifest.Protocols = append(t.manifest.Protocols, entry)
		t.xml[entry.XML] = best.data
	}

	owners := map[string]string{}
	for _, e := range t.manifest.Protocols {
		for _, it := range e.Interfaces {
			if prev, dup := owners[it.Name]; dup {
				return nil, fmt.Errorf("interface %q is defined by both %q and %q", it.Name, prev, e.Package)
			}
			owners[it.Name] = e.Package
		}
	}
	for i := range t.manifest.Protocols {
		e := &t.manifest.Protocols[i]
		deps := map[string]bool{}
		for _, ref := range parsed[e.Package].ReferencedInterfaces() {
			owner, ok := owners[ref]
			if !ok {
				return nil, fmt.Errorf("package %q references unknown interface %q", e.Package, ref)
			}
			if owner != e.Package {
				deps[owner] = true
			}
		}
		for d := range deps {
			e.Deps = append(e.Deps, d)
		}
		sort.Strings(e.Deps)
	}
	t.manifest.Sort()
	return t, nil
}

func licenseFor(pkg string) string {
	if pkg == "serverdecoration" {
		return "LGPL-2.1-or-later"
	}
	return "MIT"
}

// state is what is currently on disk under spec/.
type state struct {
	manifest *spec.Manifest
	raw      []byte
	xml      map[string][]byte
}

func readState(root string) (*state, error) {
	s := &state{manifest: &spec.Manifest{}, xml: map[string][]byte{}}
	raw, err := os.ReadFile(filepath.Join(root, "spec", "manifest.json"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		s.raw = raw
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, s.manifest); err != nil {
				return nil, fmt.Errorf("decode current manifest: %w", err)
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "spec", "xml"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, "spec", "xml", e.Name()))
		if err != nil {
			return nil, err
		}
		s.xml[e.Name()] = b
	}
	return s, nil
}

// differs reports whether t carries a real content change relative to disk:
// different XML bytes or different protocol entries. A move of a source ref
// or commit that leaves every selected XML byte-identical (and the set of
// packages unchanged) is not a change, so nothing is rewritten and no release
// is cut for it.
func (s *state) differs(t *tree) (bool, error) {
	if len(s.xml) != len(t.xml) {
		return true, nil
	}
	// A different set of upstream repositories is a change.
	if len(s.manifest.Sources) != len(t.manifest.Sources) {
		return true, nil
	}
	for i, o := range s.manifest.Sources {
		if n := t.manifest.Sources[i]; o.ID != n.ID || o.URL != n.URL {
			return true, nil
		}
	}
	// Compare manifests with the current source records substituted in.
	probe := &spec.Manifest{Sources: s.manifest.Sources, Protocols: t.manifest.Protocols}
	b, err := probe.Marshal()
	if err != nil {
		return false, err
	}
	if !bytes.Equal(s.raw, b) {
		return true, nil
	}
	for name, b := range t.xml {
		if old, ok := s.xml[name]; !ok || !bytes.Equal(old, b) {
			return true, nil
		}
	}
	return false, nil
}

// write stores t under root/spec and removes stale XML files.
func write(root string, s *state, t *tree, manifest []byte) error {
	dir := filepath.Join(root, "spec", "xml")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, b := range t.xml {
		if old, ok := s.xml[name]; ok && bytes.Equal(old, b) {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	for name := range s.xml {
		if _, keep := t.xml[name]; !keep {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
	}
	if !bytes.Equal(s.raw, manifest) {
		return os.WriteFile(filepath.Join(root, "spec", "manifest.json"), manifest, 0o644)
	}
	return nil
}
