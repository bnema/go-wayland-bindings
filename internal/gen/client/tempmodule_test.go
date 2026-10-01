package client

import (
	"os"
	"path/filepath"
	"testing"
)

// runtimeVersion is the wlturbo release that temp-module tests compile the
// generated bindings against. It is resolved from the module cache.
const runtimeVersion = "v0.5.0"

// writeTempModule writes go.mod and go.sum for a throwaway module that
// requires the published wlturbo runtime. go.sum starts from the repository's
// own go.sum; tempModuleEnv lets the go command complete it offline from the
// module cache.
func writeTempModule(t *testing.T, dir, module string) {
	t.Helper()
	mod := "module " + module + "\n\ngo 1.27\n\n" +
		"require (\n\tgithub.com/bnema/wlturbo " + runtimeVersion + "\n\tgolang.org/x/sys v0.48.0\n)\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join("..", "..", "..", "go.sum"))
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
