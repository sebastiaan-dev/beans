package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/hmans/beans/internal/output"
	"github.com/hmans/beans/internal/ui"
	"github.com/hmans/beans/internal/worktree"
	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/beangraph"
	"github.com/hmans/beans/pkg/config"
)

// startWorktreeOptions are the flags of `beans start --worktree`.
type startWorktreeOptions struct {
	worktree bool     // --worktree/-w
	name     string   // --name: worktree/branch name (default: derived from the bean)
	exec     string   // --exec/-x: program to run in the worktree
	execArgs []string // arguments for the program (after --)
	json     bool
}

// parseStartArgs splits `beans start` arguments into the bean ID and the
// arguments after `--`, which are passed to the --exec program.
func parseStartArgs(args []string, dashAt int) (id string, execArgs []string, err error) {
	positional := args
	if dashAt >= 0 {
		positional, execArgs = args[:dashAt], args[dashAt:]
	}
	if len(positional) != 1 {
		return "", nil, fmt.Errorf("accepts 1 bean ID, received %d", len(positional))
	}
	return positional[0], execArgs, nil
}

// validate checks the combination of --worktree flags.
func (o *startWorktreeOptions) validate() error {
	if !o.worktree {
		switch {
		case o.name != "":
			return errors.New("--name requires --worktree")
		case o.exec != "":
			return errors.New("--exec requires --worktree")
		case len(o.execArgs) > 0:
			return errors.New("arguments after -- require --worktree and --exec")
		}
		return nil
	}
	if len(o.execArgs) > 0 && o.exec == "" {
		return errors.New("arguments after -- require --exec")
	}
	if o.exec != "" && o.json {
		return errors.New("--exec cannot be combined with --json")
	}
	if o.name != "" {
		if err := worktree.ValidateName(o.name); err != nil {
			return err
		}
	}
	return nil
}

// maxDerivedNameLength bounds worktree names derived from beans: they become
// branch names and (with worktrunk's default template) directory names, so
// keep them readable.
const maxDerivedNameLength = 40

// worktreeNameForBean derives a worktree/branch name from a bean:
// "<id>-<slug>" ("beans-x7k2-add-login"), with the slug shortened at a word
// boundary to keep the name short. The ID is always kept whole, so names of
// different beans never collide. Falls back to a slug of the ID when it isn't
// a valid name by itself.
func worktreeNameForBean(b *bean.Bean) string {
	id := b.ID
	if worktree.ValidateName(id) != nil || strings.Contains(id, "__") {
		id = worktree.Slugify(id)
	}
	slug := b.Slug
	if slug == "" {
		slug = b.Title
	}
	slug = worktree.Slugify(slug)

	name := id
	for _, word := range strings.Split(slug, "-") {
		if word == "" {
			continue
		}
		if len(name)+1+len(word) > maxDerivedNameLength {
			break
		}
		name += "-" + word
	}
	return name
}

// newWorktreeManager returns a worktree manager configured like beans-serve's.
func newWorktreeManager(c *config.Config) (*worktree.Manager, error) {
	projectName := c.GetProjectName()
	if projectName == "" {
		projectName = filepath.Base(c.ConfigDir())
	}
	worktreeRoot, err := c.ResolveWorktreePath(projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve worktree path: %w", err)
	}
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create worktree directory %s: %w", worktreeRoot, err)
	}
	backend, err := worktree.NewBackend(string(c.GetWorktreeBackend()))
	if err != nil {
		return nil, fmt.Errorf("worktree backend: %w", err)
	}
	return worktree.NewManager(c.ConfigDir(), worktreeRoot, c.GetWorktreeBaseRef(), c.GetWorktreeSetup(),
		worktree.WithFetchTimeout(c.GetWorktreeFetchTimeout()),
		worktree.WithBackend(backend),
	), nil
}

// startWorktreeResult describes a bean started in a worktree.
type startWorktreeResult struct {
	Worktree *worktree.Worktree
	Backend  worktree.BackendKind
	Created  bool       // the worktree was created (not reused)
	Copied   bool       // the bean was copied from main into the worktree
	Bean     *bean.Bean // the worktree's copy of the bean
	Res      *transitionResult
	Warnings []string
}

// startInWorktree opens (creates or reuses) the bean's worktree, copies the
// bean into it like the web UI's Start Work, and sets the worktree's copy to
// in-progress. The bean in main is left unchanged: status changes travel with
// the branch.
func startInWorktree(ctx context.Context, mainCore *beancore.Core, mgr *worktree.Manager, id string, o *startWorktreeOptions, tio worktree.TerminalIO) (*startWorktreeResult, error) {
	mainResolver := &beangraph.CoreResolver{Core: mainCore}
	b, err := mainResolver.Bean(ctx, id)
	if err != nil {
		return nil, cmdError(o.json, output.ErrNotFound, "failed to find bean: %v", err)
	}
	if b == nil {
		return nil, cmdError(o.json, output.ErrNotFound, "bean not found: %s", id)
	}

	name := o.name
	if name == "" {
		name = worktreeNameForBean(b)
	}
	wt, created, err := mgr.Open(name, tio)
	if err != nil {
		return nil, cmdError(o.json, output.ErrFileError, "%v", err)
	}
	res := &startWorktreeResult{Worktree: wt, Backend: mgr.Backend(), Created: created}

	// Copy the bean from main (including uncommitted edits) unless the
	// worktree already changed it (vs the base ref): then the worktree's copy
	// is the current one (beans-serve shows it, and updates go to it) and
	// must not be overwritten on a rerun.
	if !containsString(mgr.DetectBeanIDs(wt.Path), b.ID) {
		if _, err := mainCore.WriteBeanToWorktree(b.ID, wt.Path); err != nil {
			return nil, cmdError(o.json, output.ErrFileError, "copying bean into worktree %s: %v", wt.Path, err)
		}
		res.Copied = true
	}

	wtCore := beancore.New(filepath.Join(wt.Path, beancore.BeansDir), mainCore.Config())
	if err := wtCore.Load(); err != nil {
		return nil, cmdError(o.json, output.ErrFileError, "loading beans in worktree %s: %v", wt.Path, err)
	}
	wtResolver := &beangraph.CoreResolver{Core: wtCore}
	tr, err := transitionBean(ctx, wtResolver, b.ID, "in-progress", "", o.json)
	if err != nil {
		return nil, err
	}
	res.Res, res.Bean = tr, tr.Bean

	warnings, err := startWarnings(ctx, wtResolver, tr.Bean)
	if err != nil {
		return nil, cmdError(o.json, output.ErrValidation, "querying beans: %v", err)
	}
	res.Warnings = warnings
	return res, nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// runStartWorktree implements `beans start <id> --worktree`.
func runStartWorktree(ctx context.Context, id string, o *startWorktreeOptions) error {
	// The worktree manager logs for beans-serve's server log; here, its
	// failures are returned as errors and progress is printed to the user.
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	mgr, err := newWorktreeManager(cfg)
	if err != nil {
		return cmdError(o.json, output.ErrValidation, "%v", err)
	}
	tio := worktree.TerminalIO{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	if o.json {
		// Keep stdout for the JSON result.
		tio.Stdout = os.Stderr
	}
	res, err := startInWorktree(ctx, core, mgr, id, o, tio)
	if err != nil {
		return err
	}

	if o.json {
		return output.JSON(output.Response{
			Success:  true,
			Bean:     res.Bean,
			Message:  startWorktreeMessage(res),
			Warnings: res.Warnings,
			Path:     res.Worktree.Path,
		})
	}
	printStartWorktree(os.Stdout, os.Stderr, res, o)

	if o.exec != "" {
		code, err := runInWorktree(res.Worktree.Path, o.exec, o.execArgs, worktree.TerminalIO{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})
		if err != nil {
			return err
		}
		if code != 0 {
			os.Exit(code)
		}
	}
	return nil
}

func startWorktreeMessage(res *startWorktreeResult) string {
	verb := "Started"
	switch {
	case res.Res.Unchanged:
		verb = "Already in-progress"
	case res.Res.Unarchived:
		verb = "Unarchived and started"
	}
	where := "existing worktree"
	if res.Created {
		where = "new worktree"
	}
	return fmt.Sprintf("%s %s in %s %s", verb, res.Bean.ID, where, res.Worktree.Branch)
}

// printStartWorktree prints the result of `beans start --worktree` and the
// next steps.
func printStartWorktree(stdout, stderr io.Writer, res *startWorktreeResult, o *startWorktreeOptions) {
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, ui.Warning.Render("Warning: ")+w)
	}
	b, wt := res.Bean, res.Worktree
	verb := "Started "
	switch {
	case res.Res.Unchanged:
		verb = "Already in-progress "
	case res.Res.Unarchived:
		verb = "Unarchived and started "
	}
	created := "existing"
	if res.Created {
		created = "new"
	}
	fmt.Fprintln(stdout, ui.Success.Render(verb)+ui.ID.Render(b.ID)+" "+b.Title)
	fmt.Fprintf(stdout, "  %s %s %s\n", ui.Muted.Render("Worktree:"), wt.Path, ui.Muted.Render("("+created+")"))
	fmt.Fprintf(stdout, "  %s %s\n", ui.Muted.Render("Branch:  "), wt.Branch)
	note := "in-progress in the worktree (main is unchanged)"
	if res.Copied {
		note = "copied into the worktree and set to in-progress there (main is unchanged)"
	}
	fmt.Fprintf(stdout, "  %s %s\n", ui.Muted.Render("Bean:    "), note)
	if o.exec != "" {
		quoted := []string{shellQuote(o.exec)}
		for _, a := range o.execArgs {
			quoted = append(quoted, shellQuote(a))
		}
		fmt.Fprintf(stdout, "\nRunning %s in the worktree...\n", strings.Join(quoted, " "))
		return
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Next steps:")
	fmt.Fprintf(stdout, "  cd %s\n", shellQuote(wt.Path))
	if res.Backend == worktree.BackendWorktrunk {
		fmt.Fprintf(stdout, "  (or: wt switch %s)\n", wt.Branch)
	}
	fmt.Fprintf(stdout, "  Launch an agent there with: beans start %s -w -x <agent>\n", b.ID)
}

// shellQuote quotes s for a POSIX shell when needed.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+:@%,", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runInWorktree runs a program in the worktree with the terminal's streams and
// returns its exit code. The program runs after the bean was prepared, so it
// sees the bean in-progress. Interrupts go to the program (it shares the
// terminal's process group); beans just waits for it.
func runInWorktree(dir, program string, args []string, tio worktree.TerminalIO) (int, error) {
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tio.Stdin, tio.Stdout, tio.Stderr

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if code := exitErr.ExitCode(); code > 0 {
				return code, nil
			}
			return 1, nil // killed by a signal
		}
		return 0, fmt.Errorf("running %s in %s: %w", program, dir, err)
	}
	return 0, nil
}
