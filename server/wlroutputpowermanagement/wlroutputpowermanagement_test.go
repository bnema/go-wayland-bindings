package wlroutputpowermanagement

import (
	"testing"

	"github.com/bnema/go-wayland-bindings/server/wayland"
)

func TestImportedInterfaces(t *testing.T) {
	req := ZwlrOutputPowerManagerV1Interface.Requests[ZwlrOutputPowerManagerV1RequestGetOutputPower]
	if got := req.Types[0]; got != ZwlrOutputPowerV1Interface {
		t.Fatalf("new_id type=%v", got)
	}
	if got := req.Types[1]; got != wayland.OutputInterface {
		t.Fatalf("output type=%v", got)
	}
}
