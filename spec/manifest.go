// Package spec holds the vendored Wayland protocol XML, the manifest that
// describes it, the shared XML model, and the package naming rule.
package spec

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
)

// ModulePath is the import path prefix of the generated bindings.
const ModulePath = "github.com/bnema/go-wayland-bindings"

//go:embed manifest.json xml
var files embed.FS

// Manifest lists upstream sources and the protocols vendored from them.
// It is written by cmd/wlbsync and never edited by hand.
type Manifest struct {
	Sources   []Source        `json:"sources"`
	Protocols []ProtocolEntry `json:"protocols"`
}

// Source is a pinned upstream repository.
type Source struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Ref    string `json:"ref"`    // tag or branch name
	Commit string `json:"commit"` // resolved commit SHA
}

// ProtocolEntry describes one vendored protocol.
type ProtocolEntry struct {
	Package    string           `json:"package"`
	XML        string           `json:"xml"`    // file name under spec/xml
	Source     string           `json:"source"` // Source.ID
	Path       string           `json:"path"`   // path inside the upstream repository
	SHA256     string           `json:"sha256"`
	License    string           `json:"license"` // SPDX identifier
	Interfaces []InterfaceEntry `json:"interfaces"`
	Deps       []string         `json:"deps"` // packages owning referenced interfaces
}

// InterfaceEntry records an interface and its highest version.
type InterfaceEntry struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// Load returns the embedded manifest.
func Load() (*Manifest, error) {
	return LoadFS(files)
}

// LoadFS reads manifest.json from fsys.
func LoadFS(fsys fs.FS) (*Manifest, error) {
	b, err := fs.ReadFile(fsys, "manifest.json")
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &m, nil
}

// Open returns the embedded XML of package pkg.
func Open(pkg string) ([]byte, error) {
	m, err := Load()
	if err != nil {
		return nil, err
	}
	return m.Open(pkg)
}

// Open returns the embedded XML of package pkg, which must be listed in m.
func (m *Manifest) Open(pkg string) ([]byte, error) {
	e, ok := m.Package(pkg)
	if !ok {
		return nil, fmt.Errorf("unknown protocol package %q", pkg)
	}
	return fs.ReadFile(files, "xml/"+e.XML)
}

// Package returns the entry named pkg.
func (m *Manifest) Package(pkg string) (ProtocolEntry, bool) {
	for _, e := range m.Protocols {
		if e.Package == pkg {
			return e, true
		}
	}
	return ProtocolEntry{}, false
}

// Owners maps every interface name to the package that defines it.
func (m *Manifest) Owners() map[string]string {
	out := make(map[string]string)
	for _, e := range m.Protocols {
		for _, it := range e.Interfaces {
			out[it.Name] = e.Package
		}
	}
	return out
}

// Lookup returns the package defining interface iface.
func (m *Manifest) Lookup(iface string) (string, bool) {
	p, ok := m.Owners()[iface]
	return p, ok
}

// Sort orders sources by ID and protocols by package, for stable output.
func (m *Manifest) Sort() {
	sort.Slice(m.Sources, func(i, j int) bool { return m.Sources[i].ID < m.Sources[j].ID })
	sort.Slice(m.Protocols, func(i, j int) bool { return m.Protocols[i].Package < m.Protocols[j].Package })
}

// Marshal encodes m as indented JSON with a trailing newline.
func (m *Manifest) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
