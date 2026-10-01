package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestGitFetcherLocal exercises the real git implementation against a local
// repository; no network is involved.
func TestGitFetcherLocal(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		out, err := git(repo, append([]string{
			"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false",
		}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	run("init", "-q", "-b", "master")
	if err := os.MkdirAll(filepath.Join(repo, "protocol"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(repo, "protocol", "wayland.xml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("one")
	run("add", ".")
	run("commit", "-q", "-m", "one")
	first := run("rev-parse", "HEAD")
	run("tag", "-a", "-m", "release", "1.0.0") // annotated: tag object != commit
	write("two")
	run("commit", "-q", "-am", "two")
	second := run("rev-parse", "HEAD")
	run("tag", "1.2.0") // lightweight
	run("tag", "1.3.1") // odd minor, patch < 90: accepted
	run("tag", "1.5.90")
	run("tag", "not-a-version")

	src := upstream{
		ID: "t", URL: repo, Kind: PinTag, TagRe: tagThreeParts, TagOK: waylandTagOK,
		Match: exactly(Stable, "protocol/wayland.xml"),
	}
	var f gitFetcher

	ref, commit, err := f.Resolve(src, "")
	if err != nil || ref != "1.3.1" || commit != second {
		t.Fatalf("latest = %q %q, %v; want 1.3.1 %s", ref, commit, err, second)
	}
	_, commit, err = f.Resolve(src, "1.0.0")
	if err != nil || commit != first {
		t.Fatalf("annotated tag resolved to %q (%v), want peeled commit %s", commit, err, first)
	}
	if _, _, err := f.Resolve(src, "9.9.9"); err == nil {
		t.Error("expected error for missing tag")
	}

	check := func(src upstream, ref, commit, want string) {
		t.Helper()
		dir := t.TempDir()
		if err := f.Checkout(src, ref, commit, dir); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "protocol", "wayland.xml"))
		if err != nil || string(b) != want {
			t.Errorf("content = %q, %v; want %q", b, err, want)
		}
	}
	check(src, "1.0.0", first, "one")
	check(src, "1.2.0", second, "two")

	// Branch source: HEAD of the branch, then a pinned older commit.
	branch := upstream{ID: "b", URL: repo, Kind: PinBranch, Branch: "master", Match: src.Match}
	ref, commit, err = f.Resolve(branch, "")
	if err != nil || ref != "master" || commit != second {
		t.Fatalf("branch = %q %q, %v", ref, commit, err)
	}
	check(branch, ref, commit, "two")
	ref, commit, err = f.Resolve(branch, first)
	if err != nil || ref != "master" || commit != first {
		t.Fatalf("pinned branch = %q %q, %v", ref, commit, err)
	}
	// Branch moved past the pinned commit: falls back to fetching the commit.
	// (Local repos need uploadpack.allowAnySHA1InWant for non-tip commits.)
	run("config", "uploadpack.allowAnySHA1InWant", "true")
	check(branch, ref, commit, "one")

	// A mismatching commit is rejected.
	if err := f.Checkout(src, "1.0.0", second, t.TempDir()); err == nil {
		t.Error("expected error when checkout does not match resolved commit")
	}
}
