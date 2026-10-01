package client

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oldWlturboEnv names the environment variable holding a checkout of the
// wlturbo repository whose checked-in bindings are the reference output.
const oldWlturboEnv = "WGB_OLD_WLTURBO"

// TestEquivalenceWithWlturboScanner regenerates every binding that the old
// wlturbo repository checks in, using the same XML, package name and -import
// mappings as its //go:generate lines, and requires byte-identical output.
// It is skipped unless WGB_OLD_WLTURBO is set.
func TestEquivalenceWithWlturboScanner(t *testing.T) {
	oldRoot := os.Getenv(oldWlturboEnv)
	if oldRoot == "" {
		t.Skipf("%s not set", oldWlturboEnv)
	}
	root := filepath.Join(oldRoot, "protocol")
	checked := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "generate.go" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(content), "\n") {
			if !strings.HasPrefix(line, "//go:generate go run ") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 5 || !strings.HasSuffix(fields[3], "/cmd/wlturbo-scanner") {
				t.Fatalf("%s: expected direct go run of cmd/wlturbo-scanner", path)
			}
			cmd := parseGenerateArgs(t, path, fields[4:])
			dir := filepath.Dir(path)
			checked++
			t.Run(cmd.pkg, func(t *testing.T) {
				xmlPath := filepath.Join(dir, cmd.input)
				data, err := os.ReadFile(xmlPath)
				if err != nil {
					t.Fatal(err)
				}
				s := NewScanner()
				s.Tool = "wlturbo-scanner"
				s.CrossPackage = cmd.imports
				if err := s.Load(data, filepath.Base(xmlPath)); err != nil {
					t.Fatal(err)
				}
				generated, err := s.Generate(cmd.pkg)
				if err != nil {
					t.Fatal(err)
				}
				checkedIn, err := os.ReadFile(filepath.Join(dir, cmd.output))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(generated, checkedIn) {
					t.Fatalf("output differs from %s", filepath.Join(dir, cmd.output))
				}
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatalf("no //go:generate lines found under %s", root)
	}
	t.Logf("compared %d generated files", checked)
}

type generateCommand struct {
	pkg, output, input string
	imports            map[string]string
}

func parseGenerateArgs(t *testing.T, path string, args []string) generateCommand {
	t.Helper()
	cmd := generateCommand{imports: map[string]string{}}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-p", "-o", "-import":
			if i+1 == len(args) {
				t.Fatalf("%s: missing value for %s", path, args[i])
			}
			flag, value := args[i], args[i+1]
			i++
			switch flag {
			case "-p":
				cmd.pkg = value
			case "-o":
				cmd.output = value
			case "-import":
				iface, target, ok := strings.Cut(value, "=")
				if !ok {
					t.Fatalf("%s: invalid import %q", path, value)
				}
				cmd.imports[iface] = target
			}
		default:
			if strings.HasPrefix(args[i], "-") || cmd.input != "" {
				t.Fatalf("%s: unexpected generation argument %q", path, args[i])
			}
			cmd.input = args[i]
		}
	}
	if cmd.pkg == "" || cmd.output == "" || cmd.input == "" {
		t.Fatalf("%s: incomplete generation command", path)
	}
	return cmd
}
