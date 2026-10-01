package wlrlayershell

import (
	"testing"

	"github.com/bnema/go-wayland-bindings/server/wayland"
	"github.com/bnema/go-wayland-bindings/server/xdgshell"
)

func TestImportedInterfaces(t *testing.T) {
	if got := ZwlrLayerShellV1Interface.Requests[0].Types[1]; got != wayland.SurfaceInterface {
		t.Fatalf("surface type=%v", got)
	}
	if got := ZwlrLayerShellV1Interface.Requests[0].Types[2]; got != wayland.OutputInterface {
		t.Fatalf("output type=%v", got)
	}
	if got := ZwlrLayerSurfaceV1Interface.Requests[5].Types[0]; got != xdgshell.PopupInterface {
		t.Fatalf("popup type=%v", got)
	}
}
