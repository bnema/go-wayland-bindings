// Command wlbcommit commits the working-tree changes of the current git
// repository to a branch on GitHub through the GraphQL createCommitOnBranch
// mutation, so that the resulting commit is signed (verified) by GitHub.
//
// Usage:
//
//	GITHUB_TOKEN=... wlbcommit -repo owner/name -branch main -message "headline\n\nbody"
//
// The new commit oid is printed on stdout.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const defaultAPI = "https://api.github.com/graphql"

const mutation = `mutation($input: CreateCommitOnBranchInput!) {
  createCommitOnBranch(input: $input) {
    commit { oid }
  }
}`

type branchInput struct {
	RepositoryNameWithOwner string `json:"repositoryNameWithOwner"`
	BranchName              string `json:"branchName"`
}

type messageInput struct {
	Headline string `json:"headline"`
	Body     string `json:"body,omitempty"`
}

type addition struct {
	Path     string `json:"path"`
	Contents string `json:"contents"`
}

type deletion struct {
	Path string `json:"path"`
}

type fileChanges struct {
	Additions []addition `json:"additions"`
	Deletions []deletion `json:"deletions"`
}

type commitInput struct {
	Branch          branchInput  `json:"branch"`
	ExpectedHeadOid string       `json:"expectedHeadOid"`
	Message         messageInput `json:"message"`
	FileChanges     fileChanges  `json:"fileChanges"`
}

type request struct {
	Query     string                 `json:"query"`
	Variables map[string]commitInput `json:"variables"`
}

type response struct {
	Data struct {
		CreateCommitOnBranch struct {
			Commit struct {
				Oid string `json:"oid"`
			} `json:"commit"`
		} `json:"createCommitOnBranch"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, "."); err != nil {
		fmt.Fprintln(os.Stderr, "wlbcommit:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer, dir string) error {
	fs := flag.NewFlagSet("wlbcommit", flag.ContinueOnError)
	repo := fs.String("repo", "", "repository as owner/name")
	branch := fs.String("branch", "", "branch to commit to")
	message := fs.String("message", "", "commit message (first line is the headline)")
	api := fs.String("api", defaultAPI, "GraphQL endpoint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *repo == "" || *branch == "" || strings.TrimSpace(*message) == "" {
		return errors.New("-repo, -branch and -message are required")
	}
	token := getenv("GITHUB_TOKEN")
	if token == "" {
		return errors.New("GITHUB_TOKEN is not set")
	}

	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	root = strings.TrimSpace(root)
	head, err := git(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	status, err := git(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return err
	}
	changes, err := collectChanges(root, status)
	if err != nil {
		return err
	}
	if len(changes.Additions) == 0 && len(changes.Deletions) == 0 {
		return errors.New("no changes to commit")
	}

	headline, body := splitMessage(*message)
	payload, err := json.Marshal(request{
		Query: mutation,
		Variables: map[string]commitInput{"input": {
			Branch:          branchInput{RepositoryNameWithOwner: *repo, BranchName: *branch},
			ExpectedHeadOid: strings.TrimSpace(head),
			Message:         messageInput{Headline: headline, Body: body},
			FileChanges:     changes,
		}},
	})
	if err != nil {
		return err
	}

	oid, err := post(ctx, *api, token, payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, oid)
	return err
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// collectChanges turns `git status --porcelain=v1 -z` output into GraphQL file
// changes. A path present in the working tree is an addition (which also
// covers modifications); an absent one is a deletion. Renames and copies yield
// an addition for the new path and, for renames, a deletion of the old one.
func collectChanges(root, status string) (fileChanges, error) {
	adds := map[string]bool{}
	dels := map[string]bool{}
	entries := strings.Split(status, "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		x, y, path := e[0], e[1], e[3:]
		adds[path] = true
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++
			if i >= len(entries) {
				return fileChanges{}, errors.New("malformed git status output")
			}
			if x == 'R' || y == 'R' {
				dels[entries[i]] = true
			}
		}
	}

	changes := fileChanges{Additions: []addition{}, Deletions: []deletion{}}
	for _, p := range sortedKeys(adds) {
		full := filepath.Join(root, filepath.FromSlash(p))
		info, err := os.Lstat(full)
		if err != nil {
			if os.IsNotExist(err) {
				dels[p] = true
				continue
			}
			return fileChanges{}, err
		}
		if info.IsDir() {
			continue // e.g. a submodule; not representable
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return fileChanges{}, err
		}
		delete(dels, p)
		changes.Additions = append(changes.Additions, addition{Path: p, Contents: base64.StdEncoding.EncodeToString(data)})
	}
	for _, p := range sortedKeys(dels) {
		if adds[p] {
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(p))); err == nil {
				continue
			}
		}
		changes.Deletions = append(changes.Deletions, deletion{Path: p})
	}
	return changes, nil
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func splitMessage(msg string) (headline, body string) {
	msg = strings.TrimSpace(msg)
	headline, body, _ = strings.Cut(msg, "\n")
	return strings.TrimSpace(headline), strings.TrimSpace(body)
}

func post(ctx context.Context, api, token string, payload []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wlbcommit")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s: %s", resp.Status, truncate(string(data)))
	}
	var out response
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(out.Errors) > 0 {
		msgs := make([]string, len(out.Errors))
		for i, e := range out.Errors {
			msgs[i] = e.Message
		}
		return "", fmt.Errorf("GraphQL errors: %s", strings.Join(msgs, "; "))
	}
	oid := out.Data.CreateCommitOnBranch.Commit.Oid
	if oid == "" {
		return "", errors.New("response contains no commit oid")
	}
	return oid, nil
}

func truncate(s string) string {
	const max = 500
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
