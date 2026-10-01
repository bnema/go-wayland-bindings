package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolateGit makes git invocations hermetic: inherited GIT_* variables are
// dropped and no user or system configuration is read.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "GIT_") {
			t.Setenv(k, "") // registers restoration of the original value
			os.Unsetenv(k)
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false",
		"-c", "user.name=test", "-c", "user.email=test@example.com",
	}, args...)
	out, err := git(context.Background(), dir, full...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRepo(t *testing.T) (dir, head string) {
	t.Helper()
	isolateGit(t)
	dir = t.TempDir()
	gitT(t, dir, "init", "-q")
	writeFile(t, dir, "keep.txt", "keep\n")
	writeFile(t, dir, "modify.txt", "old\n")
	writeFile(t, dir, "delete.txt", "bye\n")
	writeFile(t, dir, "sub/rename-me.txt", "rename content that is long enough to be detected as a rename\n")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "init")
	return dir, strings.TrimSpace(gitT(t, dir, "rev-parse", "HEAD"))
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRunRequestShape(t *testing.T) {
	dir, head := newRepo(t)
	writeFile(t, dir, "modify.txt", "new\n")
	writeFile(t, dir, "added/dir/new.bin", "\x00\x01binary")
	if err := os.Remove(filepath.Join(dir, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "mv", "sub/rename-me.txt", "renamed.txt")

	var gotAuth string
	var got request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"data":{"createCommitOnBranch":{"commit":{"oid":"abc123"}}}}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := run(context.Background(), []string{
		"-repo", "o/r", "-branch", "main", "-api", srv.URL,
		"-message", "feat: headline\n\nbody line 1\nbody line 2\n",
	}, env(map[string]string{"GITHUB_TOKEN": "tok"}), &out, dir)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "abc123\n" {
		t.Errorf("stdout = %q", out.String())
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("auth = %q", gotAuth)
	}
	if !strings.Contains(got.Query, "createCommitOnBranch") {
		t.Errorf("query = %q", got.Query)
	}
	in := got.Variables["input"]
	if in.Branch.RepositoryNameWithOwner != "o/r" || in.Branch.BranchName != "main" {
		t.Errorf("branch = %+v", in.Branch)
	}
	if in.ExpectedHeadOid != head {
		t.Errorf("expectedHeadOid = %q, want %q", in.ExpectedHeadOid, head)
	}
	if in.Message.Headline != "feat: headline" || in.Message.Body != "body line 1\nbody line 2" {
		t.Errorf("message = %+v", in.Message)
	}

	adds := map[string]string{}
	for _, a := range in.FileChanges.Additions {
		b, err := base64.StdEncoding.DecodeString(a.Contents)
		if err != nil {
			t.Fatal(err)
		}
		adds[a.Path] = string(b)
	}
	wantAdds := map[string]string{
		"modify.txt":        "new\n",
		"added/dir/new.bin": "\x00\x01binary",
		"renamed.txt":       "rename content that is long enough to be detected as a rename\n",
	}
	if len(adds) != len(wantAdds) {
		t.Errorf("additions = %v", adds)
	}
	for p, c := range wantAdds {
		if adds[p] != c {
			t.Errorf("addition %s = %q, want %q", p, adds[p], c)
		}
	}
	var dels []string
	for _, d := range in.FileChanges.Deletions {
		dels = append(dels, d.Path)
	}
	if strings.Join(dels, ",") != "delete.txt,sub/rename-me.txt" {
		t.Errorf("deletions = %v", dels)
	}
}

func TestRunEmptyBodyOmitted(t *testing.T) {
	dir, _ := newRepo(t)
	writeFile(t, dir, "x.txt", "x")
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_, _ = w.Write([]byte(`{"data":{"createCommitOnBranch":{"commit":{"oid":"o"}}}}`))
	}))
	defer srv.Close()
	err := run(context.Background(), []string{"-repo", "o/r", "-branch", "main", "-api", srv.URL, "-message", "only headline"},
		env(map[string]string{"GITHUB_TOKEN": "t"}), &bytes.Buffer{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	msg := raw["variables"].(map[string]any)["input"].(map[string]any)["message"].(map[string]any)
	if _, ok := msg["body"]; ok {
		t.Errorf("body should be omitted: %v", msg)
	}
}

func TestRunErrors(t *testing.T) {
	ok := env(map[string]string{"GITHUB_TOKEN": "t"})
	base := func(api string) []string {
		return []string{"-repo", "o/r", "-branch", "main", "-api", api, "-message", "m"}
	}
	serve := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}

	t.Run("missing token", func(t *testing.T) {
		dir, _ := newRepo(t)
		writeFile(t, dir, "x.txt", "x")
		err := run(context.Background(), base("http://127.0.0.1:1"), env(nil), &bytes.Buffer{}, dir)
		if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("missing flags", func(t *testing.T) {
		dir, _ := newRepo(t)
		err := run(context.Background(), []string{"-repo", "o/r"}, ok, &bytes.Buffer{}, dir)
		if err == nil {
			t.Error("expected error")
		}
	})
	t.Run("no changes", func(t *testing.T) {
		dir, _ := newRepo(t)
		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
		defer srv.Close()
		err := run(context.Background(), base(srv.URL), ok, &bytes.Buffer{}, dir)
		if err == nil || !strings.Contains(err.Error(), "no changes") {
			t.Errorf("err = %v", err)
		}
		if called {
			t.Error("API called despite no changes")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		dir, _ := newRepo(t)
		if err := os.Symlink("keep.txt", filepath.Join(dir, "link")); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}
		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
		defer srv.Close()
		err := run(context.Background(), base(srv.URL), ok, &bytes.Buffer{}, dir)
		if err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Errorf("err = %v", err)
		}
		if called {
			t.Error("API called despite symlink")
		}
	})
	t.Run("non-200", func(t *testing.T) {
		dir, _ := newRepo(t)
		writeFile(t, dir, "x.txt", "x")
		srv := serve(http.StatusUnauthorized, `{"message":"Bad credentials"}`)
		defer srv.Close()
		err := run(context.Background(), base(srv.URL), ok, &bytes.Buffer{}, dir)
		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("graphql errors", func(t *testing.T) {
		dir, _ := newRepo(t)
		writeFile(t, dir, "x.txt", "x")
		srv := serve(http.StatusOK, `{"data":null,"errors":[{"message":"Expected head oid mismatch"}]}`)
		defer srv.Close()
		var out bytes.Buffer
		err := run(context.Background(), base(srv.URL), ok, &out, dir)
		if err == nil || !strings.Contains(err.Error(), "Expected head oid mismatch") {
			t.Errorf("err = %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("stdout = %q", out.String())
		}
	})
	t.Run("no oid", func(t *testing.T) {
		dir, _ := newRepo(t)
		writeFile(t, dir, "x.txt", "x")
		srv := serve(http.StatusOK, `{"data":{}}`)
		defer srv.Close()
		if err := run(context.Background(), base(srv.URL), ok, &bytes.Buffer{}, dir); err == nil {
			t.Error("expected error")
		}
	})
}
