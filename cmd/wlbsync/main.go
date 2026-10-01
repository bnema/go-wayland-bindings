// Command wlbsync vendors upstream Wayland protocol XML into spec/ and writes
// spec/manifest.json.
//
// Usage:
//
//	wlbsync [-root DIR] [-check] [-notes FILE] [-pin-file FILE]
//
// With -check nothing under spec/ is written; the program prints
// "changed=true" or "changed=false". With -notes, Markdown release notes
// describing the difference to the current spec are written to FILE.
// -pin-file reads a JSON object {"<source id>": "<tag|branch|40-hex commit>"}
// overriding the default ref of each listed source.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

type options struct {
	root    string
	check   bool
	notes   string
	pinFile string
}

func main() {
	var o options
	flag.StringVar(&o.root, "root", ".", "repository root containing spec/")
	flag.BoolVar(&o.check, "check", false, "do not write spec/; print changed=true|false")
	flag.StringVar(&o.notes, "notes", "", "write Markdown release notes to `FILE`")
	flag.StringVar(&o.pinFile, "pin-file", "", "JSON file mapping source id to the ref to use")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "wlbsync: unexpected arguments")
		os.Exit(2)
	}
	if err := run(o, defaultSources(), gitFetcher{}, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "wlbsync:", err)
		os.Exit(1)
	}
}

func loadPins(file string, srcs []upstream) (map[string]string, error) {
	pins := map[string]string{}
	if file == "" {
		return pins, nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &pins); err != nil {
		return nil, fmt.Errorf("decode %s: %w", file, err)
	}
	known := map[string]bool{}
	for _, s := range srcs {
		known[s.ID] = true
	}
	for id := range pins {
		if !known[id] {
			return nil, fmt.Errorf("%s: unknown source %q", file, id)
		}
	}
	return pins, nil
}

func run(o options, srcs []upstream, f Fetcher, stdout io.Writer) error {
	pins, err := loadPins(o.pinFile, srcs)
	if err != nil {
		return err
	}
	cur, err := readState(o.root)
	if err != nil {
		return err
	}
	curParsed, err := parseOld(cur)
	if err != nil {
		return err
	}
	sources, cands, err := collect(srcs, pins, f)
	if err != nil {
		return err
	}
	t, err := build(sources, cands)
	if err != nil {
		return err
	}
	manifest, err := t.manifest.Marshal()
	if err != nil {
		return err
	}
	changed, err := cur.differs(t)
	if err != nil {
		return err
	}

	if o.notes != "" {
		n, err := diff(cur, curParsed, t)
		if err != nil {
			return err
		}
		if err := os.WriteFile(o.notes, []byte(n.markdown()), 0o644); err != nil {
			return err
		}
	}
	if changed && !o.check {
		if err := write(o.root, cur, t, manifest); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(stdout, "changed=%t\n", changed)
	return err
}
