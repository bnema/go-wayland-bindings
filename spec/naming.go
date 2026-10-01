package spec

import (
	"go/token"
	"path"
	"regexp"
	"strings"
)

var (
	unstableSuffix = regexp.MustCompile(`-unstable-v\d+$`)
	versionSuffix  = regexp.MustCompile(`-v\d+$`)
)

// PackageName returns the Go package name for an upstream protocol XML file:
// drop ".xml", then "-unstable-vN", then "-vN", then every "-".
// "wlr-layer-shell-unstable-v1.xml" becomes "wlrlayershell".
func PackageName(xmlFile string) string {
	s := strings.TrimSuffix(path.Base(xmlFile), ".xml")
	s = unstableSuffix.ReplaceAllString(s, "")
	s = versionSuffix.ReplaceAllString(s, "")
	return strings.ReplaceAll(s, "-", "")
}

// ValidPackageName reports whether name can be used as a Go package name.
func ValidPackageName(name string) bool {
	return token.IsIdentifier(name) && !token.IsKeyword(name) && name != "_"
}
