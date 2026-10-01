package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bnema/go-wayland-bindings/spec"
)

// repoRoot is the repository root relative to this package.
const repoRoot = "../../.."

// rootRequirement returns the version of module required by the repository's
// own go.mod, so the temp modules compile against the pinned release.
func rootRequirement(t *testing.T, module string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
		if len(f) >= 2 && f[0] == module {
			return f[1]
		}
	}
	t.Fatalf("go.mod has no requirement on %s", module)
	return ""
}

// writeTempModule writes go.mod and go.sum for a throwaway module that
// requires the wlturbo runtime and golang.org/x/sys at the versions pinned in
// the repository's go.mod. With selfReplace, the module also requires this
// repository and replaces it with the working tree, so generated code may
// import the generated client packages. go.sum starts from the repository's
// own go.sum; tempModuleEnv lets the go command complete it offline from the
// module cache.
func writeTempModule(t *testing.T, dir, module string, selfReplace bool) {
	t.Helper()
	mod := "module " + module + "\n\ngo 1.27\n\n" +
		"require (\n\tgithub.com/bnema/wlturbo " + rootRequirement(t, "github.com/bnema/wlturbo") +
		"\n\tgolang.org/x/sys " + rootRequirement(t, "golang.org/x/sys") + "\n)\n"
	if selfReplace {
		root, err := filepath.Abs(repoRoot)
		if err != nil {
			t.Fatal(err)
		}
		mod += "\nrequire " + spec.ModulePath + " v0.0.0\n\nreplace " + spec.ModulePath + " => " + root + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(repoRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o600); err != nil {
		t.Fatal(err)
	}
}

// tempModuleEnv resolves modules from the local module cache only: no network
// access and no checksum database lookups.
func tempModuleEnv() []string {
	return append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off", "GONOSUMDB=*", "GOSUMDB=off")
}
