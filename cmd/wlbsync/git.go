package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Fetcher is the network/git boundary of wlbsync.
type Fetcher interface {
	// Resolve returns the ref and commit for src. An empty pin selects the
	// default ref of the source (latest tag or branch HEAD); otherwise pin is
	// a tag, a branch, or a 40-hex commit.
	Resolve(src upstream, pin string) (ref, commit string, err error)
	// Checkout materialises commit of src (reached through ref) in the empty
	// directory dir.
	Checkout(src upstream, ref, commit, dir string) error
}

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// gitFetcher implements Fetcher with the git command line.
type gitFetcher struct{}

const gitTimeout = 5 * time.Minute

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// lsRemote lists the tags (peeled to commits) and branches of url.
func lsRemote(url string) (tags, heads map[string]string, err error) {
	out, err := git("", "ls-remote", "--tags", "--heads", url)
	if err != nil {
		return nil, nil, err
	}
	tags, heads = parseLsRemote(out)
	return tags, heads, nil
}

// parseLsRemote parses `git ls-remote --tags --heads` output. Annotated tags
// report the tag object and a peeled "^{}" line; the peeled commit wins.
func parseLsRemote(out string) (tags, heads map[string]string) {
	tags, heads = map[string]string{}, map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		sha, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(name, "refs/tags/"):
			tag := strings.TrimPrefix(name, "refs/tags/")
			if t, peeled := strings.CutSuffix(tag, "^{}"); peeled {
				tags[t] = sha
			} else if _, seen := tags[t]; !seen {
				tags[t] = sha
			}
		case strings.HasPrefix(name, "refs/heads/"):
			heads[strings.TrimPrefix(name, "refs/heads/")] = sha
		}
	}
	return tags, heads
}

// latestTag returns the highest tag of src accepted by its tag filters.
func latestTag(src upstream, tags map[string]string) (string, error) {
	best := ""
	for t := range tags {
		if src.TagRe != nil && !src.TagRe.MatchString(t) {
			continue
		}
		if src.TagOK != nil && !src.TagOK(t) {
			continue
		}
		if best == "" || compareVersions(t, best) > 0 {
			best = t
		}
	}
	if best == "" {
		return "", fmt.Errorf("%s: no tag matches the release pattern", src.ID)
	}
	return best, nil
}

// Resolve implements Fetcher.
func (gitFetcher) Resolve(src upstream, pin string) (string, string, error) {
	if fullSHA.MatchString(pin) {
		if src.Kind == PinBranch {
			return src.Branch, pin, nil // recorded as the branch, at the pinned commit
		}
		return pin, pin, nil
	}
	tags, heads, err := lsRemote(src.URL)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", src.ID, err)
	}
	if src.Kind == PinBranch {
		branch := src.Branch
		if pin != "" {
			branch = pin
		}
		sha, ok := heads[branch]
		if !ok {
			return "", "", fmt.Errorf("%s: branch %q not found", src.ID, branch)
		}
		return branch, sha, nil
	}
	ref := pin
	if ref == "" {
		if ref, err = latestTag(src, tags); err != nil {
			return "", "", err
		}
	}
	sha, ok := tags[ref]
	if !ok {
		return "", "", fmt.Errorf("%s: tag %q not found", src.ID, ref)
	}
	return ref, sha, nil
}

// Checkout implements Fetcher: a shallow clone of the tag or branch, falling
// back to a shallow fetch of the commit when the branch moved past a pinned
// commit or the ref is itself a commit. HEAD is verified against commit.
func (gitFetcher) Checkout(src upstream, ref, commit, dir string) error {
	if !fullSHA.MatchString(ref) {
		_, err := git("", "clone", "-q", "--depth", "1", "--branch", ref, src.URL, dir)
		if err != nil {
			return fmt.Errorf("%s: %w", src.ID, err)
		}
		if verifyHead(src, dir, commit) == nil {
			return nil
		}
		if src.Kind != PinBranch {
			return verifyHead(src, dir, commit)
		}
		// The branch moved since the commit was resolved or pinned.
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := checkoutCommit(src.URL, commit, dir); err != nil {
		return fmt.Errorf("%s: %w", src.ID, err)
	}
	return nil
}

func checkoutCommit(url, commit, dir string) error {
	steps := [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", url},
		{"fetch", "-q", "--depth", "1", "origin", commit},
		{"-c", "advice.detachedHead=false", "checkout", "-q", "FETCH_HEAD"},
	}
	for _, s := range steps {
		if _, err := git(dir, s...); err != nil {
			return err
		}
	}
	return verifyHead(upstream{ID: url}, dir, commit)
}

func verifyHead(src upstream, dir, commit string) error {
	head, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != commit {
		return fmt.Errorf("%s: checked out %s, expected %s (ref moved during sync?)", src.ID, head, commit)
	}
	return nil
}
