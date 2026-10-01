package client

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/bnema/go-wayland-bindings/spec"
)

func TestIncludedLGPLLicense(t *testing.T) {
	content, err := os.ReadFile("../../../LICENSES/LGPL-2.1-or-later.txt")
	if err != nil {
		t.Fatal(err)
	}
	const expected = "5749785c8bdefafcb5d798270ed0a967036fe2ca63dcedade1627565dfef81d2"
	if got := fmt.Sprintf("%x", sha256.Sum256(content)); got != expected {
		t.Fatalf("LGPL text checksum %s, want %s", got, expected)
	}
}

func TestGeneratedSourceRetainsProtocolLicense(t *testing.T) {
	for _, c := range []struct{ pkg, source string }{
		{"serverdecoration", "server-decoration.xml"},
		{"wlrlayershell", "wlr-layer-shell-unstable-v1.xml"},
		{"wayland", "wayland.xml"},
	} {
		t.Run(c.pkg, func(t *testing.T) {
			data, err := spec.Open(c.pkg)
			if err != nil {
				t.Fatal(err)
			}
			s := NewScanner()
			if err := s.Load(data, c.source); err != nil {
				t.Fatal(err)
			}
			s.CrossPackage = map[string]string{
				"wl_surface": "github.com/bnema/go-wayland-bindings/client/wayland",
				"wl_output":  "github.com/bnema/go-wayland-bindings/client/wayland",
				"xdg_popup":  "github.com/bnema/go-wayland-bindings/client/xdgshell",
			}
			generated, err := s.Generate("wayland")
			if err != nil {
				t.Fatal(err)
			}
			header, _, ok := strings.Cut(string(generated), "\npackage wayland")
			if !ok {
				t.Fatal("package declaration missing")
			}
			if !strings.Contains(header, "// Source: "+c.source+"\n") {
				t.Errorf("source name missing from header:\n%s", header)
			}
			for _, line := range strings.Split(strings.TrimSpace(s.protocol.Copyright), "\n") {
				if text := strings.TrimSpace(line); text != "" && !strings.Contains(header, text) {
					t.Errorf("upstream license line missing from header: %q", text)
				}
			}
		})
	}
}
