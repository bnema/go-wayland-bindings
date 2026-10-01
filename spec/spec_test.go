package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestManifestInvariants(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Protocols) == 0 {
		t.Fatal("manifest has no protocols; run `go run ./cmd/wlbsync`")
	}
	if len(m.Sources) == 0 {
		t.Fatal("manifest has no sources")
	}

	sources := map[string]bool{}
	for _, s := range m.Sources {
		if s.ID == "" || s.URL == "" || s.Ref == "" || len(s.Commit) != 40 {
			t.Errorf("incomplete source %+v", s)
		}
		sources[s.ID] = true
	}

	owners := map[string]string{}
	parsed := map[string]*Protocol{}
	listed := map[string]bool{}
	var pkgs []string

	for _, e := range m.Protocols {
		pkgs = append(pkgs, e.Package)
		listed[e.XML] = true
		if !sources[e.Source] {
			t.Errorf("%s: unknown source %q", e.Package, e.Source)
		}
		if !ValidPackageName(e.Package) {
			t.Errorf("%s: invalid package name", e.Package)
		}
		if e.XML != e.Package+".xml" {
			t.Errorf("%s: xml file %q, want %q", e.Package, e.XML, e.Package+".xml")
		}
		if e.Package != "wayland" && PackageName(path.Base(e.Path)) != e.Package {
			t.Errorf("%s: PackageName(%q) = %q", e.Package, e.Path, PackageName(path.Base(e.Path)))
		}
		wantLicense := "MIT"
		if e.Package == "serverdecoration" {
			wantLicense = "LGPL-2.1-or-later"
		}
		if e.License != wantLicense {
			t.Errorf("%s: license %q, want %q", e.Package, e.License, wantLicense)
		}

		data, err := Open(e.Package)
		if err != nil {
			t.Errorf("%s: %v", e.Package, err)
			continue
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != e.SHA256 {
			t.Errorf("%s: sha256 %s, manifest has %s", e.Package, got, e.SHA256)
		}
		if !strings.Contains(string(data), "<copyright>") {
			t.Errorf("%s: missing <copyright>", e.Package)
		}
		p, err := Parse(data)
		if err != nil {
			t.Errorf("%s: %v", e.Package, err)
			continue
		}
		parsed[e.Package] = p

		var want []InterfaceEntry
		for _, it := range p.Interfaces {
			want = append(want, InterfaceEntry{Name: it.Name, Version: it.Version})
			if prev, dup := owners[it.Name]; dup {
				t.Errorf("interface %s owned by both %s and %s", it.Name, prev, e.Package)
			}
			owners[it.Name] = e.Package
		}
		sort.Slice(want, func(i, j int) bool { return want[i].Name < want[j].Name })
		if !reflect.DeepEqual(e.Interfaces, want) {
			t.Errorf("%s: manifest interfaces %v, XML has %v", e.Package, e.Interfaces, want)
		}
	}
	if !sort.StringsAreSorted(pkgs) {
		t.Error("manifest protocols are not sorted by package")
	}

	for _, e := range m.Protocols {
		p := parsed[e.Package]
		if p == nil {
			continue
		}
		depSet := map[string]bool{}
		for _, ref := range p.ReferencedInterfaces() {
			owner, ok := owners[ref]
			if !ok {
				t.Errorf("%s references unknown interface %s", e.Package, ref)
				continue
			}
			if owner != e.Package {
				depSet[owner] = true
			}
		}
		want := []string{}
		for d := range depSet {
			want = append(want, d)
		}
		sort.Strings(want)
		if !reflect.DeepEqual(e.Deps, want) {
			t.Errorf("%s: deps %v, want %v", e.Package, e.Deps, want)
		}
	}

	entries, err := files.ReadDir("xml")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range entries {
		if !listed[f.Name()] {
			t.Errorf("spec/xml/%s is not in the manifest", f.Name())
		}
	}
	if len(entries) != len(m.Protocols) {
		t.Errorf("%d files in spec/xml, %d manifest entries", len(entries), len(m.Protocols))
	}
}

func TestManifestAPI(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if pkg, ok := m.Lookup("wl_surface"); !ok || pkg != "wayland" {
		t.Errorf("Lookup(wl_surface) = %q, %v", pkg, ok)
	}
	if pkg, ok := m.Lookup("xdg_wm_base"); !ok || pkg != "xdgshell" {
		t.Errorf("Lookup(xdg_wm_base) = %q, %v", pkg, ok)
	}
	if _, ok := m.Lookup("nope"); ok {
		t.Error("Lookup(nope) succeeded")
	}
	if _, err := Open("nope"); err == nil {
		t.Error("Open(nope) succeeded")
	}
	if _, ok := m.Package("xdgshell"); !ok {
		t.Error("Package(xdgshell) not found")
	}
}
