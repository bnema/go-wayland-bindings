package spec

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func findInterface(t *testing.T, p *Protocol, name string) Interface {
	t.Helper()
	for _, it := range p.Interfaces {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("interface %q not found", name)
	return Interface{}
}

func findMessage(t *testing.T, ms []Message, name string) Message {
	t.Helper()
	for _, m := range ms {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("message %q not found", name)
	return Message{}
}

func findEnum(t *testing.T, it Interface, name string) Enum {
	t.Helper()
	for _, e := range it.Enums {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("enum %q not found in %s", name, it.Name)
	return Enum{}
}

func TestParseWayland(t *testing.T) {
	p, err := Parse(readTestdata(t, "wayland.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "wayland" {
		t.Errorf("Name = %q", p.Name)
	}
	if !strings.Contains(p.Copyright, "Kristian") {
		t.Errorf("Copyright not preserved: %q", p.Copyright)
	}
	if len(p.Interfaces) != 23 {
		t.Errorf("got %d interfaces, want 23", len(p.Interfaces))
	}
	if got := findInterface(t, p, "wl_compositor").Version; got != 7 {
		t.Errorf("wl_compositor version = %d, want 7", got)
	}

	// since on a message.
	release := findMessage(t, findInterface(t, p, "wl_pointer").Requests, "release")
	if release.Type != "destructor" || release.Since != 3 {
		t.Errorf("wl_pointer.release = type %q since %d", release.Type, release.Since)
	}
	if got := findMessage(t, findInterface(t, p, "wl_pointer").Requests, "set_cursor").SinceOrOne(); got != 1 {
		t.Errorf("SinceOrOne of unversioned message = %d, want 1", got)
	}
	if got := release.SinceOrOne(); got != 3 {
		t.Errorf("SinceOrOne = %d, want 3", got)
	}

	// since on an enum.
	if e := findEnum(t, findInterface(t, p, "wl_data_device_manager"), "dnd_action"); !e.Bitfield || e.Since != 3 {
		t.Errorf("dnd_action = bitfield %v since %d, want true 3", e.Bitfield, e.Since)
	}

	// bitfield.
	seat := findInterface(t, p, "wl_seat")
	if !findEnum(t, seat, "capability").Bitfield {
		t.Error("wl_seat.capability should be a bitfield")
	}
	if findEnum(t, seat, "error").Bitfield {
		t.Error("wl_seat.error should not be a bitfield")
	}
	if len(findEnum(t, seat, "capability").Entries) == 0 {
		t.Error("capability has no entries")
	}

	// allow-null and interface refs.
	var nullArg, plainArg *Arg
	for _, m := range findInterface(t, p, "wl_data_device").Requests {
		for i := range m.Args {
			a := &m.Args[i]
			if m.Name == "set_selection" && a.Name == "source" {
				nullArg = a
			}
			if m.Name == "start_drag" && a.Name == "origin" {
				plainArg = a
			}
		}
	}
	if nullArg == nil || !nullArg.AllowNull || nullArg.Interface != "wl_data_source" || nullArg.Type != "object" {
		t.Errorf("set_selection.source = %+v", nullArg)
	}
	if plainArg == nil || plainArg.AllowNull {
		t.Errorf("start_drag.origin = %+v", plainArg)
	}

	// enum refs, qualified and unqualified.
	create := findMessage(t, findInterface(t, p, "wl_shm_pool").Requests, "create_buffer")
	var enumRefs []string
	for _, a := range create.Args {
		if a.Enum != "" {
			enumRefs = append(enumRefs, a.Enum)
		}
	}
	if !reflect.DeepEqual(enumRefs, []string{"wl_shm.format"}) {
		t.Errorf("create_buffer enum refs = %v", enumRefs)
	}
	var found bool
	for _, a := range findMessage(t, findInterface(t, p, "wl_output").Events, "mode").Args {
		if a.Name == "flags" && a.Enum == "mode" && a.Summary != "" {
			found = true
		}
	}
	if !found {
		t.Error("wl_output.mode flags enum ref/summary not preserved")
	}

	// Descriptions.
	if d := findInterface(t, p, "wl_display").Description; d == nil || d.Summary == "" || strings.TrimSpace(d.Text) == "" {
		t.Errorf("description not parsed: %+v", d)
	}
}

func TestParseFixture(t *testing.T) {
	p, err := Parse(readTestdata(t, "protocol_fixture.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "wlvision_fixture" || !strings.Contains(p.Copyright, "Synthetic protocol") {
		t.Errorf("protocol = %q / %q", p.Name, p.Copyright)
	}
	if len(p.Interfaces) != 3 {
		t.Fatalf("got %d interfaces", len(p.Interfaces))
	}
	mgr := findInterface(t, p, "zwlr_fixture_manager_v1")
	if mgr.Version != 2 || len(mgr.Requests) != 10 {
		t.Errorf("manager version %d requests %d", mgr.Version, len(mgr.Requests))
	}
	out := findMessage(t, mgr.Requests, "set_optional_output").Args[0]
	if !out.AllowNull || out.Interface != "wl_output" {
		t.Errorf("set_optional_output arg = %+v", out)
	}
	thing := findInterface(t, p, "zwlr_fixture_thing_v1")
	if len(thing.Events) != 7 || len(thing.Enums) != 1 || thing.Enums[0].Entries[1].Value != "1" {
		t.Errorf("thing = %d events, enums %+v", len(thing.Events), thing.Enums)
	}
	want := []string{"wl_seat", "zwlr_fixture_thing_v1", "wl_region", "wl_buffer", "wl_output", "zwlr_fixture_mode_v1", "wl_surface"}
	if got := p.ReferencedInterfaces(); !reflect.DeepEqual(got, want) {
		t.Errorf("ReferencedInterfaces = %v, want %v", got, want)
	}
}

func TestParseErrors(t *testing.T) {
	for name, in := range map[string]string{
		"not xml":   "nope",
		"no name":   `<protocol></protocol>`,
		"wrong tag": `<other name="x"/>`,
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
