package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/bnema/go-wayland-bindings/spec"
)

// fakeFetcher serves source trees from memory; no network or git.
type fakeFetcher struct {
	// trees[source id][ref] = repository files.
	trees map[string]map[string]map[string]string
	// latest[source id] = ref used when no pin is given.
	latest map[string]string
}

func (f *fakeFetcher) commit(id, ref string) string {
	return fmt.Sprintf("%040x", len(id)*1000+len(ref)) // deterministic, 40 chars
}

func (f *fakeFetcher) Resolve(src upstream, pin string) (string, string, error) {
	ref := pin
	if ref == "" {
		ref = f.latest[src.ID]
	}
	if _, ok := f.trees[src.ID][ref]; !ok {
		return "", "", fmt.Errorf("fake: %s has no ref %q", src.ID, ref)
	}
	return ref, f.commit(src.ID, ref), nil
}

func (f *fakeFetcher) Checkout(src upstream, ref, commit, dir string) error {
	files, ok := f.trees[src.ID][ref]
	if !ok {
		return fmt.Errorf("fake: %s has no ref %q", src.ID, ref)
	}
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// xmlDoc builds a protocol document. Each interface is "name:version" followed
// by optional requests "req(arg:type[:iface],...)".
func xmlDoc(name string, copyright bool, ifaces ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<protocol name=%q>\n", name)
	if copyright {
		b.WriteString("  <copyright>Copyright test</copyright>\n")
	}
	for _, spec := range ifaces {
		parts := strings.Split(spec, "|")
		nv := strings.SplitN(parts[0], ":", 2)
		fmt.Fprintf(&b, "  <interface name=%q version=%q>\n", nv[0], nv[1])
		for _, req := range parts[1:] {
			open := strings.Index(req, "(")
			fmt.Fprintf(&b, "    <request name=%q>\n", req[:open])
			args := strings.TrimSuffix(req[open+1:], ")")
			if args != "" {
				for _, a := range strings.Split(args, ",") {
					f := strings.Split(a, ":")
					if len(f) == 3 {
						fmt.Fprintf(&b, "      <arg name=%q type=%q interface=%q/>\n", f[0], f[1], f[2])
					} else {
						fmt.Fprintf(&b, "      <arg name=%q type=%q/>\n", f[0], f[1])
					}
				}
			}
			b.WriteString("    </request>\n")
		}
		b.WriteString("  </interface>\n")
	}
	b.WriteString("</protocol>\n")
	return b.String()
}

func testSources() []upstream {
	var out []upstream
	for _, s := range defaultSources() {
		if s.ID == "wayland" || s.ID == "wayland-protocols" {
			out = append(out, s)
		}
	}
	return out
}

func waylandTree() map[string]string {
	return map[string]string{
		"protocol/wayland.xml": xmlDoc("wayland", true, "wl_surface:6", "wl_seat:9"),
		"protocol/other.xml":   "ignored: not a vendored path",
	}
}

func baseFetcher() *fakeFetcher {
	return &fakeFetcher{
		trees: map[string]map[string]map[string]string{
			"wayland": {"1.0.0": waylandTree()},
			"wayland-protocols": {
				"1.0": {
					"stable/xdg-shell/xdg-shell.xml": xmlDoc("xdg_shell", true,
						"xdg_wm_base:5|get_surface(id:new_id:xdg_surface,surface:object:wl_surface)",
						"xdg_surface:5"),
					"staging/foo/foo-v1.xml": xmlDoc("foo", true, "foo_manager_v1:1|make(seat:object:wl_seat)"),
					"stable/tests/skip.xml":  xmlDoc("skip", false),
					"experimental/x/xx.xml":  xmlDoc("xx", false),
					"unstable/old/old-unstable-v1.xml": xmlDoc("old", true,
						"old_thing_v1:1|poke(n:uint)"),
					"README.md": "not xml",
				},
			},
		},
		latest: map[string]string{"wayland": "1.0.0", "wayland-protocols": "1.0"},
	}
}

func syncOnce(t *testing.T, root string, f Fetcher, o options) (string, error) {
	t.Helper()
	o.root = root
	var out bytes.Buffer
	err := run(o, testSources(), f, &out)
	return strings.TrimSpace(out.String()), err
}

func mustSync(t *testing.T, root string, f Fetcher, o options) string {
	t.Helper()
	out, err := syncOnce(t, root, f, o)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		out[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func readNotes(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func loadRoot(t *testing.T, root string) *spec.Manifest {
	t.Helper()
	m, err := spec.LoadFS(os.DirFS(filepath.Join(root, "spec")))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestInitialSync(t *testing.T) {
	root := t.TempDir()
	f := baseFetcher()
	if got := mustSync(t, root, f, options{}); got != "changed=true" {
		t.Fatalf("output %q", got)
	}
	m := loadRoot(t, root)

	var pkgs []string
	for _, e := range m.Protocols {
		pkgs = append(pkgs, e.Package)
	}
	if want := []string{"foo", "old", "wayland", "xdgshell"}; !reflect.DeepEqual(pkgs, want) {
		t.Fatalf("packages = %v, want %v", pkgs, want)
	}

	xdg, _ := m.Package("xdgshell")
	if xdg.Source != "wayland-protocols" || xdg.Path != "stable/xdg-shell/xdg-shell.xml" || xdg.License != "MIT" {
		t.Errorf("xdgshell = %+v", xdg)
	}
	if want := []string{"wayland"}; !reflect.DeepEqual(xdg.Deps, want) {
		t.Errorf("xdgshell deps = %v", xdg.Deps)
	}
	if want := []spec.InterfaceEntry{{Name: "xdg_surface", Version: 5}, {Name: "xdg_wm_base", Version: 5}}; !reflect.DeepEqual(xdg.Interfaces, want) {
		t.Errorf("xdgshell interfaces = %v", xdg.Interfaces)
	}
	if w, _ := m.Package("wayland"); len(w.Deps) != 0 || w.Deps == nil {
		t.Errorf("wayland deps = %#v, want empty non-nil", w.Deps)
	}
	if len(m.Sources) != 2 || m.Sources[0].ID != "wayland" || m.Sources[1].Ref != "1.0" {
		t.Errorf("sources = %+v", m.Sources)
	}

	// Bytes are copied unchanged, and the sha256 describes them.
	got, err := os.ReadFile(filepath.Join(root, "spec", "xml", "xdgshell.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != f.trees["wayland-protocols"]["1.0"]["stable/xdg-shell/xdg-shell.xml"] {
		t.Error("xml bytes differ from upstream")
	}
	if len(xdg.SHA256) != 64 {
		t.Errorf("sha256 = %q", xdg.SHA256)
	}
	if _, err := os.Stat(filepath.Join(root, "spec", "xml", "skip.xml")); err == nil {
		t.Error("tests/ file was vendored")
	}

	// Second run: nothing changes.
	before := snapshot(t, root)
	if got := mustSync(t, root, f, options{}); got != "changed=false" {
		t.Errorf("rerun output %q", got)
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Error("rerun modified spec/")
	}
}

func TestLicenseServerDecoration(t *testing.T) {
	var src []upstream
	for _, s := range defaultSources() {
		if s.ID == "plasma-wayland-protocols" {
			src = append(src, s)
		}
	}
	f := &fakeFetcher{
		trees: map[string]map[string]map[string]string{"plasma-wayland-protocols": {"v1.0.0": {
			"src/protocols/server-decoration.xml": xmlDoc("server_decoration", true, "org_kde_kwin_server_decoration:1"),
		}}},
		latest: map[string]string{"plasma-wayland-protocols": "v1.0.0"},
	}
	root := t.TempDir()
	if err := run(options{root: root}, src, f, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	e, ok := loadRoot(t, root).Package("serverdecoration")
	if !ok || e.License != "LGPL-2.1-or-later" {
		t.Errorf("entry = %+v, %v", e, ok)
	}
}

func TestCheckWritesNothing(t *testing.T) {
	root := t.TempDir()
	f := baseFetcher()

	// Empty tree: check reports a change but creates nothing.
	if got := mustSync(t, root, f, options{check: true}); got != "changed=true" {
		t.Errorf("output %q", got)
	}
	if len(snapshot(t, root)) != 0 {
		t.Errorf("-check wrote files: %v", snapshot(t, root))
	}

	mustSync(t, root, f, options{})
	if got := mustSync(t, root, f, options{check: true}); got != "changed=false" {
		t.Errorf("output %q", got)
	}

	// Upstream moves on; -check reports it and leaves everything alone,
	// while -notes still works.
	f.trees["wayland-protocols"]["1.1"] = map[string]string{
		"stable/xdg-shell/xdg-shell.xml": xmlDoc("xdg_shell", true, "xdg_wm_base:6", "xdg_surface:5"),
		"staging/foo/foo-v1.xml":         f.trees["wayland-protocols"]["1.0"]["staging/foo/foo-v1.xml"],
	}
	f.latest["wayland-protocols"] = "1.1"
	before := snapshot(t, root)
	notesFile := filepath.Join(t.TempDir(), "notes.md")
	if got := mustSync(t, root, f, options{check: true, notes: notesFile}); got != "changed=true" {
		t.Errorf("output %q", got)
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Error("-check modified spec/")
	}
	if n := readNotes(t, notesFile); !strings.Contains(n, "xdg_wm_base v5 → v6") {
		t.Errorf("notes missing bump:\n%s", n)
	}
}

func TestNotes(t *testing.T) {
	root := t.TempDir()
	f := baseFetcher()
	mustSync(t, root, f, options{})

	// New upstream release:
	//  - xdg_wm_base version bump, request get_surface keeps its args
	//  - xdg_popup interface added
	//  - foo-v2 replaces foo-v1 (major replacement)
	//  - old package removed
	//  - new package "bar" added
	//  - wayland: new tag, identical content (only the source ref changes)
	f.trees["wayland-protocols"]["1.1"] = map[string]string{
		"stable/xdg-shell/xdg-shell.xml": xmlDoc("xdg_shell", true,
			"xdg_wm_base:6|get_surface(id:new_id:xdg_surface,surface:object:wl_surface)",
			"xdg_surface:5",
			"xdg_popup:5"),
		"staging/foo/foo-v1.xml": f.trees["wayland-protocols"]["1.0"]["staging/foo/foo-v1.xml"],
		"staging/foo/foo-v2.xml": xmlDoc("foo", true, "foo_manager_v2:1|make(seat:object:wl_seat)"),
		"staging/bar/bar-v1.xml": xmlDoc("bar", true, "bar_manager_v1:1"),
	}
	f.trees["wayland"]["1.0.1"] = map[string]string{
		"protocol/wayland.xml": xmlDoc("wayland", true, "wl_surface:6", "wl_seat:9"),
	}
	f.latest["wayland-protocols"] = "1.1"
	f.latest["wayland"] = "1.0.1"

	notesFile := filepath.Join(t.TempDir(), "notes.md")
	mustSync(t, root, f, options{notes: notesFile})
	n := readNotes(t, notesFile)

	for _, want := range []string{
		"## BREAKING",
		"- old: package removed",
		"- foo: major replacement, wayland-protocols:staging/foo/foo-v1.xml → wayland-protocols:staging/foo/foo-v2.xml",
		"- foo: interface foo_manager_v1 removed",
		"## Sources",
		"- wayland-protocols: 1.0 → 1.1",
		"- wayland: 1.0.0 → 1.0.1",
		"## Added packages",
		"- `bar`",
		"## Removed packages",
		"- `old`",
		"## New interfaces",
		"- xdgshell: xdg_popup (v5)",
		"## Interface version bumps",
		"- xdgshell: xdg_wm_base v5 → v6",
	} {
		if !strings.Contains(n, want) {
			t.Errorf("notes missing %q:\n%s", want, n)
		}
	}
	if strings.Contains(n, "get_surface") {
		t.Errorf("unchanged message reported:\n%s", n)
	}
	// Stale XML removed, new XML present.
	if _, err := os.Stat(filepath.Join(root, "spec", "xml", "old.xml")); err == nil {
		t.Error("stale old.xml not removed")
	}
	if _, err := os.Stat(filepath.Join(root, "spec", "xml", "bar.xml")); err != nil {
		t.Error("bar.xml missing")
	}
	m := loadRoot(t, root)
	if foo, _ := m.Package("foo"); foo.Path != "staging/foo/foo-v2.xml" {
		t.Errorf("foo path = %s", foo.Path)
	}

	// Rerun against the same upstream: zero changes, notes say so.
	if got := mustSync(t, root, f, options{notes: notesFile}); got != "changed=false" {
		t.Errorf("rerun output %q", got)
	}
	if n := readNotes(t, notesFile); !strings.Contains(n, "No changes.") {
		t.Errorf("notes for unchanged sync:\n%s", n)
	}
}

func TestBreakingMessageChanges(t *testing.T) {
	root := t.TempDir()
	f := baseFetcher()
	mustSync(t, root, f, options{})

	f.trees["wayland-protocols"]["1.1"] = map[string]string{
		"stable/xdg-shell/xdg-shell.xml": xmlDoc("xdg_shell", true,
			// get_surface: arg 2 interface changes; surface type kept.
			"xdg_wm_base:5|get_surface(id:new_id:xdg_surface,surface:object:wl_seat)|extra(a:int)",
			// xdg_surface unchanged.
			"xdg_surface:5"),
		"staging/foo/foo-v1.xml": xmlDoc("foo", true,
			// make: arg count changes.
			"foo_manager_v1:1|make(seat:object:wl_seat,n:uint)"),
		"unstable/old/old-unstable-v1.xml": xmlDoc("old", true,
			// poke removed, arg type of nothing; new request added.
			"old_thing_v1:1|prod(n:uint)"),
	}
	f.latest["wayland-protocols"] = "1.1"
	notesFile := filepath.Join(t.TempDir(), "notes.md")
	mustSync(t, root, f, options{notes: notesFile})
	n := readNotes(t, notesFile)
	for _, want := range []string{
		"- xdgshell: xdg_wm_base request get_surface arguments changed: #2 surface:object<wl_surface> → surface:object<wl_seat>",
		"- foo: foo_manager_v1 request make arguments changed: 1 → 2 arguments",
		"- old: old_thing_v1 request poke removed",
	} {
		if !strings.Contains(n, want) {
			t.Errorf("notes missing %q:\n%s", want, n)
		}
	}
	if strings.Contains(n, "request extra") || strings.Contains(n, "prod") {
		t.Errorf("added requests must not be breaking:\n%s", n)
	}
}

func TestBreakingEventAndRename(t *testing.T) {
	old := &spec.Protocol{Interfaces: []spec.Interface{{
		Name:   "a",
		Events: []spec.Message{{Name: "e", Args: []spec.Arg{{Name: "x", Type: "int"}}}},
	}}}
	cur := &spec.Protocol{Interfaces: []spec.Interface{{
		Name:   "a",
		Events: []spec.Message{{Name: "e", Args: []spec.Arg{{Name: "y", Type: "int"}}}, {Name: "f"}},
	}}}
	n := &notes{}
	diffProtocol(n, "p", old, cur)
	if len(n.breaking) != 1 || !strings.Contains(n.breaking[0], "event e arguments changed: #1 x:int → y:int") {
		t.Errorf("breaking = %v", n.breaking)
	}
	n = &notes{}
	diffProtocol(n, "p", cur, old)
	if len(n.breaking) != 2 || !strings.Contains(n.breaking[1], "event f removed") {
		t.Errorf("breaking = %v", n.breaking)
	}
}

func TestSelectionRule(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string // selected path
	}{
		{
			name: "stable beats staging beats unstable",
			files: map[string]string{
				"stable/s/foo.xml":               xmlDoc("a", true, "a_s:1"),
				"staging/s/foo-v3.xml":           xmlDoc("b", true, "b_s:1"),
				"unstable/s/foo-unstable-v4.xml": xmlDoc("c", true, "c_s:1"),
			},
			want: "stable/s/foo.xml",
		},
		{
			name: "higher major wins within a stability",
			files: map[string]string{
				"staging/s/foo-v1.xml":  xmlDoc("a", true, "a_s:1"),
				"staging/s/foo-v2.xml":  xmlDoc("b", true, "b_s:1"),
				"staging/s/foo-v10.xml": xmlDoc("c", true, "c_s:1"),
			},
			want: "staging/s/foo-v10.xml",
		},
		{
			name: "no suffix counts as v1",
			files: map[string]string{
				"staging/s/foo.xml":    xmlDoc("a", true, "a_s:1"),
				"staging/s/foo-v2.xml": xmlDoc("b", true, "b_s:1"),
			},
			want: "staging/s/foo-v2.xml",
		},
		{
			name: "unstable suffix dropped before version",
			files: map[string]string{
				"unstable/s/foo-unstable-v1.xml": xmlDoc("a", true, "a_s:1"),
				"staging/s/foo-v1.xml":           xmlDoc("b", true, "b_s:1"),
			},
			want: "staging/s/foo-v1.xml",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeFetcher{
				trees:  map[string]map[string]map[string]string{"wayland-protocols": {"1.0": tc.files}},
				latest: map[string]string{"wayland-protocols": "1.0"},
			}
			root := t.TempDir()
			src := testSources()[1:]
			if err := run(options{root: root}, src, f, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			e, ok := loadRoot(t, root).Package("foo")
			if !ok || e.Path != tc.want {
				t.Errorf("selected %q, want %q", e.Path, tc.want)
			}
		})
	}
}

func TestCollisions(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		wantErr string
	}{
		{
			name: "tie on stability and major",
			files: map[string]string{
				"stable/a/foo-v1.xml": xmlDoc("a", true, "a_s:1"),
				"stable/b/foo.xml":    xmlDoc("b", true, "b_s:1"),
			},
			wantErr: `package "foo"`,
		},
		{
			name: "same interface in two packages",
			files: map[string]string{
				"stable/a/foo.xml": xmlDoc("a", true, "shared:1"),
				"stable/b/bar.xml": xmlDoc("b", true, "shared:1"),
			},
			wantErr: `interface "shared" is defined by both`,
		},
		{
			name: "missing copyright",
			files: map[string]string{
				"stable/a/foo.xml": xmlDoc("a", false, "a_s:1"),
			},
			wantErr: "missing <copyright>",
		},
		{
			name: "unparsable xml",
			files: map[string]string{
				"stable/a/foo.xml": "<protocol",
			},
			wantErr: "foo.xml",
		},
		{
			name: "unknown referenced interface",
			files: map[string]string{
				"stable/a/foo.xml": xmlDoc("a", true, "a_s:1|r(x:object:ghost)"),
			},
			wantErr: `unknown interface "ghost"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeFetcher{
				trees:  map[string]map[string]map[string]string{"wayland-protocols": {"1.0": tc.files}},
				latest: map[string]string{"wayland-protocols": "1.0"},
			}
			root := t.TempDir()
			err := run(options{root: root}, testSources()[1:], f, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
			if len(snapshot(t, root)) != 0 {
				t.Error("files written despite error")
			}
		})
	}
}

func TestErrorsLeaveSpecUntouched(t *testing.T) {
	root := t.TempDir()
	f := baseFetcher()
	mustSync(t, root, f, options{})
	before := snapshot(t, root)

	f.trees["wayland-protocols"]["1.1"] = map[string]string{
		"stable/xdg-shell/xdg-shell.xml": xmlDoc("xdg_shell", false, "xdg_wm_base:5"),
	}
	f.latest["wayland-protocols"] = "1.1"
	if _, err := syncOnce(t, root, f, options{}); err == nil {
		t.Fatal("expected error")
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Error("spec/ modified after failed sync")
	}

	// Unknown ref is an error too.
	if _, err := syncOnce(t, root, f, options{pinFile: writePins(t, `{"wayland":"9.9.9"}`)}); err == nil {
		t.Error("expected error for unknown pin")
	}
}

func writePins(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pins.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPinFile(t *testing.T) {
	root := t.TempDir()
	f := baseFetcher()
	f.trees["wayland-protocols"]["1.1"] = map[string]string{
		"stable/xdg-shell/xdg-shell.xml": xmlDoc("xdg_shell", true, "xdg_wm_base:6"),
	}
	f.latest["wayland-protocols"] = "1.1"

	// Pin to the older ref although 1.1 is latest.
	mustSync(t, root, f, options{pinFile: writePins(t, `{"wayland-protocols":"1.0"}`)})
	m := loadRoot(t, root)
	if m.Sources[1].Ref != "1.0" {
		t.Errorf("ref = %s, want pinned 1.0", m.Sources[1].Ref)
	}
	if e, _ := m.Package("xdgshell"); e.Interfaces[0].Version != 5 && e.Interfaces[1].Version != 5 {
		t.Errorf("pinned content not used: %+v", e)
	}

	// Without the pin the latest is used.
	mustSync(t, root, f, options{})
	if got := loadRoot(t, root).Sources[1].Ref; got != "1.1" {
		t.Errorf("ref = %s, want 1.1", got)
	}

	if _, err := syncOnce(t, root, f, options{pinFile: writePins(t, `{"nope":"1"}`)}); err == nil || !strings.Contains(err.Error(), "unknown source") {
		t.Errorf("unknown source error = %v", err)
	}
	if _, err := syncOnce(t, root, f, options{pinFile: writePins(t, `[`)}); err == nil {
		t.Error("expected decode error")
	}
}

func TestInitialPinsFile(t *testing.T) {
	pins, err := loadPins("testdata/initial-pins.json", defaultSources())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"wayland":                  "1.26.0",
		"wayland-protocols":        "1.49",
		"wlr-protocols":            "bf4fc79abc359eea5a0edec0ac6d4a2b2955f82a",
		"wlroots":                  "0.20.2",
		"plasma-wayland-protocols": "v1.23.0",
	}
	if !reflect.DeepEqual(pins, want) {
		t.Errorf("pins = %v", pins)
	}
}

func TestNoXMLFound(t *testing.T) {
	f := &fakeFetcher{
		trees:  map[string]map[string]map[string]string{"wayland": {"1.0.0": {"README": "x"}}},
		latest: map[string]string{"wayland": "1.0.0"},
	}
	err := run(options{root: t.TempDir()}, testSources()[:1], f, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no protocol XML") {
		t.Errorf("error = %v", err)
	}
}

func TestSourceTable(t *testing.T) {
	got := map[string]upstream{}
	for _, s := range defaultSources() {
		got[s.ID] = s
	}
	urls := map[string]string{
		"wayland":                  "https://gitlab.freedesktop.org/wayland/wayland.git",
		"wayland-protocols":        "https://gitlab.freedesktop.org/wayland/wayland-protocols.git",
		"wlr-protocols":            "https://gitlab.freedesktop.org/wlroots/wlr-protocols.git",
		"wlroots":                  "https://gitlab.freedesktop.org/wlroots/wlroots.git",
		"plasma-wayland-protocols": "https://invent.kde.org/libraries/plasma-wayland-protocols.git",
	}
	for id, url := range urls {
		if got[id].URL != url {
			t.Errorf("%s url = %q", id, got[id].URL)
		}
	}
	if len(got) != len(urls) {
		t.Errorf("%d sources", len(got))
	}
	if s := got["wlr-protocols"]; s.Kind != PinBranch || s.Branch != "master" {
		t.Errorf("wlr-protocols = %+v", s)
	}

	type m struct {
		id, path string
		ok       bool
		st       Stability
	}
	for _, c := range []m{
		{"wayland", "protocol/wayland.xml", true, Stable},
		{"wayland", "protocol/other.xml", false, 0},
		{"wayland-protocols", "stable/xdg-shell/xdg-shell.xml", true, Stable},
		{"wayland-protocols", "staging/xdg-dialog/xdg-dialog-v1.xml", true, Staging},
		{"wayland-protocols", "unstable/tablet/tablet-unstable-v2.xml", true, Unstable},
		{"wayland-protocols", "stable/tests/x.xml", false, 0},
		{"wayland-protocols", "staging/experimental/x.xml", false, 0},
		{"wayland-protocols", "experimental/xx-shell/xx-shell-v1.xml", false, 0},
		{"wayland-protocols", "stable/xdg-shell/README.md", false, 0},
		{"wlr-protocols", "unstable/wlr-layer-shell-unstable-v1.xml", true, Unstable},
		{"wlr-protocols", "unstable/sub/x.xml", false, 0},
		{"wlr-protocols", "stable/x.xml", false, 0},
		{"wlroots", "protocol/input-method-unstable-v2.xml", true, Unstable},
		{"wlroots", "protocol/virtual-keyboard-unstable-v1.xml", true, Unstable},
		{"wlroots", "protocol/wlr-layer-shell-unstable-v1.xml", false, 0},
		{"plasma-wayland-protocols", "src/protocols/server-decoration.xml", true, Unstable},
		{"plasma-wayland-protocols", "src/protocols/idle.xml", false, 0},
	} {
		st, ok := got[c.id].Match(c.path)
		if ok != c.ok || st != c.st {
			t.Errorf("%s Match(%q) = %v,%v want %v,%v", c.id, c.path, st, ok, c.st, c.ok)
		}
	}
}

func TestTagSelection(t *testing.T) {
	tags := map[string]string{}
	for _, tg := range []string{"1.2.0", "1.10.0", "1.10.90", "1.11.91", "1.11.1", "1.9.0", "1.9.95", "1.10.0-rc1", "v1.12.0", "1.27.0-rc1"} {
		tags[tg] = "sha-" + tg
	}
	var wayland, protocols, plasma upstream
	for _, s := range defaultSources() {
		switch s.ID {
		case "wayland":
			wayland = s
		case "wayland-protocols":
			protocols = s
		case "plasma-wayland-protocols":
			plasma = s
		}
	}
	// Even minor or patch<90: 1.10.90 allowed (even minor), 1.11.91 and 1.9.95 are not,
	// 1.11.1 is. Numeric compare: 1.10.90 > 1.11.1? No: 1.11.1 > 1.10.90.
	if got, err := latestTag(wayland, tags); err != nil || got != "1.11.1" {
		t.Errorf("wayland latest = %q, %v", got, err)
	}
	delete(tags, "1.11.1")
	if got, _ := latestTag(wayland, tags); got != "1.10.90" {
		t.Errorf("wayland latest = %q", got)
	}

	if got, err := latestTag(protocols, map[string]string{"1.9": "a", "1.10": "b", "1.49": "c", "1.49.1": "d", "v1.50": "e"}); err != nil || got != "1.49" {
		t.Errorf("wayland-protocols latest = %q, %v", got, err)
	}
	if got, err := latestTag(plasma, map[string]string{"v1.9.0": "a", "v1.23.0": "b", "1.99.0": "c"}); err != nil || got != "v1.23.0" {
		t.Errorf("plasma latest = %q, %v", got, err)
	}
	if _, err := latestTag(plasma, map[string]string{"1.0.0": "a"}); err == nil {
		t.Error("expected error when no tag matches")
	}
}

func TestParseLsRemote(t *testing.T) {
	out := strings.Join([]string{
		"aaaa\trefs/heads/master",
		"bbbb\trefs/tags/1.0",
		"cccc\trefs/tags/1.0^{}",
		"dddd\trefs/tags/1.1",
		"",
	}, "\n")
	tags, heads := parseLsRemote(out)
	if want := map[string]string{"1.0": "cccc", "1.1": "dddd"}; !reflect.DeepEqual(tags, want) {
		t.Errorf("tags = %v (annotated tags must resolve to the peeled commit)", tags)
	}
	if want := map[string]string{"master": "aaaa"}; !reflect.DeepEqual(heads, want) {
		t.Errorf("heads = %v", heads)
	}
	// Peeled line before the tag object line.
	tags, _ = parseLsRemote("cccc\trefs/tags/2.0^{}\nbbbb\trefs/tags/2.0")
	if tags["2.0"] != "cccc" {
		t.Errorf("tags = %v", tags)
	}
}

func TestFileMajorAndVersions(t *testing.T) {
	for in, want := range map[string]int{
		"foo.xml": 1, "foo-v2.xml": 2, "stable/x/foo-unstable-v3.xml": 3, "a-v12.xml": 12, "v2-thing.xml": 1,
	} {
		if got := fileMajor(in); got != want {
			t.Errorf("fileMajor(%q) = %d, want %d", in, got, want)
		}
	}
	vs := []string{"1.10", "1.2", "1.9"}
	sort.Slice(vs, func(i, j int) bool { return compareVersions(vs[i], vs[j]) < 0 })
	if !reflect.DeepEqual(vs, []string{"1.2", "1.9", "1.10"}) {
		t.Errorf("sorted = %v", vs)
	}
	if compareVersions("v1.2.3", "1.2.3") != 0 {
		t.Error("v prefix should be ignored")
	}
}
