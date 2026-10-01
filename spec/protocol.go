package spec

import (
	"encoding/xml"
	"fmt"
)

// Protocol is a parsed Wayland protocol XML document.
type Protocol struct {
	XMLName     xml.Name     `xml:"protocol"`
	Name        string       `xml:"name,attr"`
	Copyright   string       `xml:"copyright"`
	Description *Description `xml:"description"`
	Interfaces  []Interface  `xml:"interface"`
}

// Interface is a versioned Wayland interface.
type Interface struct {
	Name        string       `xml:"name,attr"`
	Version     int          `xml:"version,attr"`
	Description *Description `xml:"description"`
	Requests    []Message    `xml:"request"`
	Events      []Message    `xml:"event"`
	Enums       []Enum       `xml:"enum"`
}

// Message is a request (client to server) or an event (server to client).
type Message struct {
	Name        string       `xml:"name,attr"`
	Type        string       `xml:"type,attr"` // "destructor" or empty
	Since       int          `xml:"since,attr"`
	Description *Description `xml:"description"`
	Args        []Arg        `xml:"arg"`
}

// Arg is a message argument.
type Arg struct {
	Name      string `xml:"name,attr"`
	Type      string `xml:"type,attr"`
	Summary   string `xml:"summary,attr"`
	Interface string `xml:"interface,attr"`
	AllowNull bool   `xml:"allow-null,attr"`
	Enum      string `xml:"enum,attr"`
}

// Enum is an enumeration or bitfield.
type Enum struct {
	Name        string       `xml:"name,attr"`
	Since       int          `xml:"since,attr"`
	Bitfield    bool         `xml:"bitfield,attr"`
	Description *Description `xml:"description"`
	Entries     []Entry      `xml:"entry"`
}

// Entry is an enum value.
type Entry struct {
	Name    string `xml:"name,attr"`
	Value   string `xml:"value,attr"`
	Summary string `xml:"summary,attr"`
	Since   int    `xml:"since,attr"`
}

// Description is protocol documentation.
type Description struct {
	Summary string `xml:"summary,attr"`
	Text    string `xml:",chardata"`
}

// Parse decodes a protocol XML document.
func Parse(data []byte) (*Protocol, error) {
	var p Protocol
	if err := xml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse protocol: %w", err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("parse protocol: missing protocol name")
	}
	return &p, nil
}

// ReferencedInterfaces returns the interfaces named by arguments of p, in
// first-seen order, without duplicates.
func (p *Protocol) ReferencedInterfaces() []string {
	seen := map[string]bool{}
	var out []string
	add := func(ms []Message) {
		for _, m := range ms {
			for _, a := range m.Args {
				if a.Interface != "" && !seen[a.Interface] {
					seen[a.Interface] = true
					out = append(out, a.Interface)
				}
			}
		}
	}
	for _, it := range p.Interfaces {
		add(it.Requests)
		add(it.Events)
	}
	return out
}
