package main

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Stability ranks upstream protocol maturity; higher wins when several files
// map to one package.
type Stability int

// Stability levels.
const (
	Unstable Stability = iota + 1
	Staging
	Stable
)

// PinKind says how the default ref of a source is chosen.
type PinKind int

const (
	// PinTag follows the latest tag accepted by upstream.TagRe and upstream.TagOK.
	PinTag PinKind = iota
	// PinBranch follows the HEAD commit of upstream.Branch.
	PinBranch
)

// upstream describes an upstream repository and which of its files are vendored.
type upstream struct {
	ID  string
	URL string

	Kind   PinKind
	TagRe  *regexp.Regexp        // PinTag: tags must match
	TagOK  func(tag string) bool // PinTag: optional extra filter
	Branch string                // PinBranch: branch name

	// Match reports whether the slash-separated repository path rel is a
	// vendored protocol, and its stability.
	Match func(rel string) (Stability, bool)
}

var (
	tagThreeParts = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	tagTwoParts   = regexp.MustCompile(`^\d+\.\d+$`)
	tagVThreePart = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
)

// waylandTagOK accepts release tags X.Y.Z only; patch 90 and above are
// release candidates regardless of the minor version.
func waylandTagOK(tag string) bool {
	v := versionParts(tag)
	return len(v) == 3 && v[2] < 90
}

// exactly matches only the listed repository paths, all with stability s.
func exactly(s Stability, paths ...string) func(string) (Stability, bool) {
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
	}
	return func(rel string) (Stability, bool) {
		if !set[rel] {
			return 0, false
		}
		return s, true
	}
}

func matchWaylandProtocols(rel string) (Stability, bool) {
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || !strings.HasSuffix(rel, ".xml") {
		return 0, false
	}
	for _, p := range parts[:len(parts)-1] {
		if p == "tests" || p == "experimental" {
			return 0, false
		}
	}
	switch parts[0] {
	case "stable":
		return Stable, true
	case "staging":
		return Staging, true
	case "unstable":
		return Unstable, true
	}
	return 0, false
}

func matchWlrProtocols(rel string) (Stability, bool) {
	if path.Dir(rel) == "unstable" && strings.HasSuffix(rel, ".xml") {
		return Unstable, true
	}
	return 0, false
}

// defaultSources lists the upstream repositories synced into spec/.
func defaultSources() []upstream {
	return []upstream{
		{
			ID:    "wayland",
			URL:   "https://gitlab.freedesktop.org/wayland/wayland.git",
			Kind:  PinTag,
			TagRe: tagThreeParts,
			TagOK: waylandTagOK,
			Match: exactly(Stable, "protocol/wayland.xml"),
		},
		{
			ID:    "wayland-protocols",
			URL:   "https://gitlab.freedesktop.org/wayland/wayland-protocols.git",
			Kind:  PinTag,
			TagRe: tagTwoParts,
			Match: matchWaylandProtocols,
		},
		{
			ID:     "wlr-protocols",
			URL:    "https://gitlab.freedesktop.org/wlroots/wlr-protocols.git",
			Kind:   PinBranch,
			Branch: "master",
			Match:  matchWlrProtocols,
		},
		{
			ID:    "wlroots",
			URL:   "https://gitlab.freedesktop.org/wlroots/wlroots.git",
			Kind:  PinTag,
			TagRe: tagThreeParts,
			Match: exactly(Unstable,
				"protocol/input-method-unstable-v2.xml",
				"protocol/virtual-keyboard-unstable-v1.xml"),
		},
		{
			ID:    "plasma-wayland-protocols",
			URL:   "https://invent.kde.org/libraries/plasma-wayland-protocols.git",
			Kind:  PinTag,
			TagRe: tagVThreePart,
			Match: exactly(Unstable, "src/protocols/server-decoration.xml"),
		},
	}
}

// versionParts splits "v1.2.3" or "1.2" into its numeric components.
func versionParts(tag string) []int {
	var out []int
	for _, f := range strings.Split(strings.TrimPrefix(tag, "v"), ".") {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// compareVersions orders two tags numerically.
func compareVersions(a, b string) int {
	x, y := versionParts(a), versionParts(b)
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			if x[i] < y[i] {
				return -1
			}
			return 1
		}
	}
	return len(x) - len(y)
}

var majorSuffix = regexp.MustCompile(`-v(\d+)$`)

// fileMajor returns the "vN" major of an XML file name; none means 1.
func fileMajor(file string) int {
	base := strings.TrimSuffix(path.Base(file), ".xml")
	if m := majorSuffix.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 1
}
