package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bnema/go-wayland-bindings/spec"
)

// notes is the difference between the vendored spec and a new tree.
type notes struct {
	sources   []string // "id: old → new"
	added     []string // package names
	removed   []string
	bumps     []string // "pkg: iface vA → vB"
	newIfaces []string // "pkg: iface (vN)"
	updated   []string // packages whose XML bytes changed
	breaking  []string
}

func (n *notes) empty() bool {
	return len(n.sources)+len(n.added)+len(n.removed)+len(n.bumps)+
		len(n.newIfaces)+len(n.updated)+len(n.breaking) == 0
}

// parseOld parses the currently vendored XML of every manifest entry.
func parseOld(s *state) (map[string]*spec.Protocol, error) {
	out := map[string]*spec.Protocol{}
	for _, e := range s.manifest.Protocols {
		data, ok := s.xml[e.XML]
		if !ok {
			return nil, fmt.Errorf("current manifest lists %s but spec/xml/%s is missing", e.Package, e.XML)
		}
		p, err := spec.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("current %s: %w", e.XML, err)
		}
		out[e.Package] = p
	}
	return out, nil
}

// diff compares the on-disk state with the desired tree.
func diff(old *state, oldParsed map[string]*spec.Protocol, t *tree) (*notes, error) {
	n := &notes{}

	oldSrc := map[string]spec.Source{}
	for _, s := range old.manifest.Sources {
		oldSrc[s.ID] = s
	}
	newSrc := map[string]spec.Source{}
	for _, s := range t.manifest.Sources {
		newSrc[s.ID] = s
		switch o, ok := oldSrc[s.ID]; {
		case !ok:
			n.sources = append(n.sources, fmt.Sprintf("%s: added at %s", s.ID, s.Ref))
		case o.Ref != s.Ref:
			n.sources = append(n.sources, fmt.Sprintf("%s: %s → %s", s.ID, o.Ref, s.Ref))
		case o.Commit != s.Commit:
			n.sources = append(n.sources, fmt.Sprintf("%s: %s %s → %s", s.ID, s.Ref, short(o.Commit), short(s.Commit)))
		}
	}
	for _, s := range old.manifest.Sources {
		if _, ok := newSrc[s.ID]; !ok {
			n.sources = append(n.sources, fmt.Sprintf("%s: removed", s.ID))
		}
	}

	oldEntries := map[string]spec.ProtocolEntry{}
	for _, e := range old.manifest.Protocols {
		oldEntries[e.Package] = e
	}
	newEntries := map[string]spec.ProtocolEntry{}
	for _, e := range t.manifest.Protocols {
		newEntries[e.Package] = e
	}

	for _, e := range t.manifest.Protocols {
		o, existed := oldEntries[e.Package]
		if !existed {
			n.added = append(n.added, e.Package)
			continue
		}
		if o.SHA256 != e.SHA256 {
			n.updated = append(n.updated, e.Package)
		}
		if o.Source != e.Source || o.Path != e.Path {
			n.breaking = append(n.breaking, fmt.Sprintf("%s: major replacement, %s:%s → %s:%s",
				e.Package, o.Source, o.Path, e.Source, e.Path))
		}
		newP, err := spec.Parse(t.xml[e.XML])
		if err != nil {
			return nil, err
		}
		diffProtocol(n, e.Package, oldParsed[e.Package], newP)
	}
	for _, e := range old.manifest.Protocols {
		if _, ok := newEntries[e.Package]; !ok {
			n.removed = append(n.removed, e.Package)
			n.breaking = append(n.breaking, fmt.Sprintf("%s: package removed", e.Package))
		}
	}

	for _, l := range []*[]string{&n.sources, &n.added, &n.removed, &n.bumps, &n.newIfaces, &n.updated, &n.breaking} {
		sort.Strings(*l)
	}
	return n, nil
}

func short(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func diffProtocol(n *notes, pkg string, old, cur *spec.Protocol) {
	oldIf := map[string]spec.Interface{}
	for _, it := range old.Interfaces {
		oldIf[it.Name] = it
	}
	seen := map[string]bool{}
	for _, it := range cur.Interfaces {
		seen[it.Name] = true
		o, ok := oldIf[it.Name]
		if !ok {
			n.newIfaces = append(n.newIfaces, fmt.Sprintf("%s: %s (v%d)", pkg, it.Name, it.Version))
			continue
		}
		if o.Version != it.Version {
			n.bumps = append(n.bumps, fmt.Sprintf("%s: %s v%d → v%d", pkg, it.Name, o.Version, it.Version))
		}
		diffMessages(n, pkg, it.Name, "request", o.Requests, it.Requests)
		diffMessages(n, pkg, it.Name, "event", o.Events, it.Events)
	}
	for _, it := range old.Interfaces {
		if !seen[it.Name] {
			n.breaking = append(n.breaking, fmt.Sprintf("%s: interface %s removed", pkg, it.Name))
		}
	}
}

func diffMessages(n *notes, pkg, iface, kind string, old, cur []spec.Message) {
	curByName := map[string]spec.Message{}
	for _, m := range cur {
		curByName[m.Name] = m
	}
	for _, o := range old {
		m, ok := curByName[o.Name]
		if !ok {
			n.breaking = append(n.breaking, fmt.Sprintf("%s: %s %s %s removed", pkg, iface, kind, o.Name))
			continue
		}
		if change := argsChange(o.Args, m.Args); change != "" {
			n.breaking = append(n.breaking, fmt.Sprintf("%s: %s %s %s arguments changed: %s", pkg, iface, kind, o.Name, change))
		}
	}
}

// argsChange describes the first difference between two argument lists,
// comparing name, type and interface by position. It returns "" when equal.
func argsChange(old, cur []spec.Arg) string {
	if len(old) != len(cur) {
		return fmt.Sprintf("%d → %d arguments", len(old), len(cur))
	}
	var diffs []string
	for i := range old {
		o, c := old[i], cur[i]
		if o.Name != c.Name || o.Type != c.Type || o.Interface != c.Interface {
			diffs = append(diffs, fmt.Sprintf("#%d %s → %s", i+1, argString(o), argString(c)))
		}
	}
	return strings.Join(diffs, "; ")
}

func argString(a spec.Arg) string {
	s := a.Name + ":" + a.Type
	if a.Interface != "" {
		s += "<" + a.Interface + ">"
	}
	return s
}

// markdown renders the release notes.
func (n *notes) markdown() string {
	var b strings.Builder
	b.WriteString("# Upstream sync\n\n")
	if n.empty() {
		b.WriteString("No changes.\n")
		return b.String()
	}
	section := func(title string, items []string, code bool) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "## %s\n\n", title)
		for _, it := range items {
			if code {
				fmt.Fprintf(&b, "- `%s`\n", it)
			} else {
				fmt.Fprintf(&b, "- %s\n", it)
			}
		}
		b.WriteString("\n")
	}
	section("BREAKING", n.breaking, false)
	section("Sources", n.sources, false)
	section("Added packages", n.added, true)
	section("Removed packages", n.removed, true)
	section("New interfaces", n.newIfaces, false)
	section("Interface version bumps", n.bumps, false)
	section("Updated XML", n.updated, true)
	return strings.TrimRight(b.String(), "\n") + "\n"
}
