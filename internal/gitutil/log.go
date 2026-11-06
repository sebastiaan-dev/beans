package gitutil

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// Commit is a single commit as reported by ScanCommits.
type Commit struct {
	Hash      string // full commit hash
	ShortHash string // abbreviated commit hash
	Subject   string // first line of the commit message
	Message   string // full raw commit message (subject + body)
}

// IsRepo reports whether dir is inside a git work tree.
func IsRepo(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// RefExists reports whether ref resolves to a commit in the repo at dir.
func RefExists(dir, ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return false
	}
	return exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run() == nil
}

// ScanCommits streams the commits reachable from ref (newest first) to visit,
// using a single `git log` invocation. If since is non-empty it is passed to
// `git log --since` (any date format git understands, e.g. "2024-01-01" or
// "3 months ago"). Returning false from visit stops the scan early.
//
// Output is streamed rather than buffered, so memory use stays bounded on
// large histories.
func ScanCommits(dir, ref, since string, visit func(Commit) bool) error {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return fmt.Errorf("invalid ref %q", ref)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// -z separates commits with NUL; fields are separated with the ASCII unit
	// separator. Neither byte can appear in a commit message.
	args := []string{"-C", dir, "log", "-z", "--no-color", "--format=%H%x1f%h%x1f%B"}
	if since != "" {
		args = append(args, "--since="+since)
	}
	args = append(args, ref, "--")

	cmd := exec.CommandContext(ctx, "git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git log: %w", err)
	}

	stopped := false
	reader := bufio.NewReaderSize(stdout, 64*1024)
	for {
		record, readErr := reader.ReadString(0)
		record = strings.TrimSuffix(record, "\x00")
		if record != "" {
			if c, ok := parseCommitRecord(record); ok && !visit(c) {
				stopped = true
				break
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				cancel()
				_ = cmd.Wait()
				return fmt.Errorf("reading git log: %w", readErr)
			}
			break
		}
	}

	if stopped {
		cancel()
		_ = cmd.Wait()
		return nil
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git log %s: %w: %s", ref, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// parseCommitRecord parses one "%H\x1f%h\x1f%B" record.
func parseCommitRecord(record string) (Commit, bool) {
	// git separates -z records with NUL but may emit a leading newline
	// between records; strip surrounding whitespace from the hash.
	parts := strings.SplitN(record, "\x1f", 3)
	if len(parts) != 3 {
		return Commit{}, false
	}
	msg := strings.TrimRight(parts[2], "\n")
	subject, _, _ := strings.Cut(msg, "\n")
	return Commit{
		Hash:      strings.TrimSpace(parts[0]),
		ShortHash: strings.TrimSpace(parts[1]),
		Subject:   strings.TrimSpace(subject),
		Message:   msg,
	}, true
}

// TopLevel returns the absolute root directory of the work tree containing dir.
func TopLevel(dir string) (string, error) {
	return gitRevParse(dir, "--show-toplevel")
}

// ChangedPaths returns the repo-relative paths changed by each of the given
// commits, keyed by full hash, using a single `git log --no-walk` call.
// Root commits are compared against the empty tree; merge commits report no
// paths (git does not diff merges by default). Hashes must be full commit
// hashes.
func ChangedPaths(dir string, hashes []string) (map[string][]string, error) {
	result := make(map[string][]string, len(hashes))
	if len(hashes) == 0 {
		return result, nil
	}
	cmd := exec.Command("git", "-C", dir, "-c", "core.quotePath=false",
		"log", "--no-walk=unsorted", "--stdin", "--no-color", "--format=%x1e%H", "--name-only")
	cmd.Stdin = strings.NewReader(strings.Join(hashes, "\n") + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log --name-only: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	for _, record := range strings.Split(string(out), "\x1e") {
		lines := strings.Split(strings.TrimSpace(record), "\n")
		if len(lines) == 0 || lines[0] == "" {
			continue
		}
		hash := strings.TrimSpace(lines[0])
		paths := []string{}
		for _, l := range lines[1:] {
			if l = strings.TrimSpace(l); l != "" {
				paths = append(paths, l)
			}
		}
		result[hash] = paths
	}
	return result, nil
}

// RepoRelativePaths converts paths to slash-separated paths relative to the
// root of the git work tree containing dir, dropping empty paths and paths
// outside of it. Relative paths are resolved against the working directory.
func RepoRelativePaths(dir string, paths []string) []string {
	top, err := TopLevel(dir)
	if err != nil {
		return nil
	}
	resolve := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		// The path may not exist (yet); resolve its parent instead.
		if r, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
			return filepath.Join(r, filepath.Base(p))
		}
		return p
	}
	top = resolve(top)
	var rels []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		rel, err := filepath.Rel(top, resolve(p))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		rels = append(rels, filepath.ToSlash(rel))
	}
	return rels
}

// CommitsChangingOnly returns the commits (keys of paths, as returned by
// ChangedPaths) whose changed files all match one of rules: repo-relative
// paths of a file, or of a directory matching everything below it. Commits
// with no reported paths (empty or merge commits) never match.
func CommitsChangingOnly(paths map[string][]string, rules []string) map[string]bool {
	matches := func(f string) bool {
		for _, r := range rules {
			r = strings.TrimSuffix(r, "/")
			if f == r || strings.HasPrefix(f, r+"/") {
				return true
			}
		}
		return false
	}
	result := make(map[string]bool)
	for hash, files := range paths {
		if len(files) == 0 {
			continue
		}
		all := true
		for _, f := range files {
			if !matches(f) {
				all = false
				break
			}
		}
		if all {
			result[hash] = true
		}
	}
	return result
}
