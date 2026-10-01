package server

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bnema/go-wayland-bindings/spec"
)

// TestEquivalence regenerates every protocol listed in the //go:generate lines
// of the baseline purego-libwayland protocol/gen.go and requires byte-identical
// output. It runs only when WGB_OLD_PUREGO points at a baseline checkout.
func TestEquivalence(t *testing.T) {
	root := os.Getenv("WGB_OLD_PUREGO")
	if root == "" {
		t.Skip("WGB_OLD_PUREGO not set")
	}
	protoDir := filepath.Join(root, "protocol")
	f, err := os.Open(filepath.Join(protoDir, "gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	const prefix = "//go:generate go run ../cmd/wlgen "
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		args := strings.Fields(strings.TrimPrefix(line, prefix))
		n++
		t.Run(line[len(prefix):], func(t *testing.T) { checkLine(t, protoDir, args) })
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no //go:generate lines found")
	}
}

func loadXML(t *testing.T, path string) *spec.Protocol {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := spec.Parse(b)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return p
}

func checkLine(t *testing.T, protoDir string, args []string) {
	opts := Options{Imports: map[string]string{}, Owners: map[string]string{}, Tool: "wlgen"}
	var out, xmlPath string
	var importXML []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-package", "-out", "-import", "-import-xml":
			if i+1 >= len(args) {
				t.Fatalf("flag %s missing value", args[i])
			}
			v := args[i+1]
			i++
			switch args[i-1] {
			case "-package":
				opts.Package = v
			case "-out":
				out = v
			case "-import":
				a, path, ok := strings.Cut(v, "=")
				if !ok {
					t.Fatalf("invalid -import %q", v)
				}
				opts.Imports[a] = path
			case "-import-xml":
				importXML = append(importXML, v)
			}
		default:
			xmlPath = args[i]
		}
	}
	if opts.Package == "" || out == "" || xmlPath == "" {
		t.Fatalf("incomplete generate line: %v", args)
	}
	for _, s := range importXML {
		a, path, ok := strings.Cut(s, "=")
		if !ok || opts.Imports[a] == "" {
			t.Fatalf("invalid -import-xml %q", s)
		}
		for _, it := range loadXML(t, filepath.Join(protoDir, path)).Interfaces {
			opts.Owners[it.Name] = a
		}
	}
	got, err := Generate(loadXML(t, filepath.Join(protoDir, xmlPath)), opts)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(protoDir, out))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("generated output differs from %s (got %d bytes, want %d)", out, len(got), len(want))
	}
}
