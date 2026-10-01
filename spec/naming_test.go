package spec

import "testing"

func TestPackageName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"wlr-layer-shell-unstable-v1.xml", "wlrlayershell"},
		{"tablet-v2.xml", "tablet"},
		{"presentation-time.xml", "presentationtime"},
		{"xdg-shell.xml", "xdgshell"},
		{"wayland.xml", "wayland"},
		{"ext-foreign-toplevel-list-v1.xml", "extforeigntoplevellist"},
		{"xdg-foreign-unstable-v2.xml", "xdgforeign"},
		{"stable/xdg-shell/xdg-shell.xml", "xdgshell"},
		{"server-decoration.xml", "serverdecoration"},
	}
	for _, tt := range tests {
		got := PackageName(tt.in)
		if got != tt.want {
			t.Errorf("PackageName(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if !ValidPackageName(got) {
			t.Errorf("PackageName(%q) = %q is not a valid Go package name", tt.in, got)
		}
	}
}

func TestValidPackageName(t *testing.T) {
	for _, name := range []string{"wayland", "xdgshell", "a1"} {
		if !ValidPackageName(name) {
			t.Errorf("ValidPackageName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "_", "1a", "a-b", "func", "type", "a.b"} {
		if ValidPackageName(name) {
			t.Errorf("ValidPackageName(%q) = true, want false", name)
		}
	}
}
