package commands

import (
	"fmt"
	"os"

	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
	"github.com/spf13/cobra"
)

var core *beancore.Core
var cfg *config.Config
var beansPath string
var configPath string

// NewRootCmd creates the root cobra command with shared persistent flags
// and core initialization logic.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "beans",
		Short: "A file-based issue tracker for AI-first workflows",
		Long: `Beans is a lightweight issue tracker that stores issues as markdown files.
Track your work alongside your code and supercharge your coding agent with
a full view of your project.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if !requiresProject(cmd) {
				return nil
			}

			var err error
			cfg, err = loadConfig(configPath)
			if err != nil {
				return err
			}

			root, err := resolveBeansPath(beansPath, cfg)
			if err != nil {
				return err
			}

			core = beancore.New(root, cfg)
			if err := core.Load(); err != nil {
				return fmt.Errorf("loading beans: %w", err)
			}

			return nil
		},
	}

	rootCmd.PersistentFlags().StringVar(&beansPath, "beans-path", "", "Path to data directory (overrides config and BEANS_PATH env var)")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to config file (overrides BEANS_CONFIG env var; default: searches upward for .beans.yml)")

	return rootCmd
}

// requiresProject reports whether cmd needs a loaded beans project.
// Commands that work outside of a project (setup, help, shell completion
// scripts) must not fail just because no .beans directory exists.
func requiresProject(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "init", "prime", "version", "help", "completion",
			cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return false
		}
	}
	return true
}

// configEnvVar names the environment variable equivalent of the --config flag.
const configEnvVar = "BEANS_CONFIG"

// resolveConfigFile determines which config file to use.
// Precedence: --config flag > BEANS_CONFIG env var > upward search for
// .beans.yml starting at startDir.
//
// An explicitly configured file (flag or env var) must exist; otherwise an
// error is returned rather than silently falling back to defaults. When no
// explicit file is configured and the upward search finds nothing, it returns
// an empty path and no error.
func resolveConfigFile(flagPath, startDir string) (string, error) {
	path, source := flagPath, "--config"
	if path == "" {
		path, source = os.Getenv(configEnvVar), configEnvVar
	}
	if path == "" {
		return config.FindConfig(startDir)
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("config file set via %s does not exist: %s", source, path)
		}
		return "", fmt.Errorf("config file set via %s is not accessible: %w", source, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("config file set via %s is a directory, not a file: %s", source, path)
	}
	return path, nil
}

// loadConfig loads the configuration selected by resolveConfigFile. Without
// any config file, it returns the defaults anchored at the current directory.
func loadConfig(flagPath string) (*config.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting current directory: %w", err)
	}

	path, err := resolveConfigFile(flagPath, cwd)
	if err != nil {
		return nil, err
	}
	if path == "" {
		c := config.Default()
		c.SetConfigDir(cwd)
		return c, nil
	}

	c, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading config from %s: %w", path, err)
	}
	return c, nil
}

// resolveBeansPath determines the beans data directory path.
// Precedence: --beans-path flag > BEANS_PATH env var > config default.
//
// In worktrees, the CLI uses the worktree's local .beans/ directory.
// beans-serve watches worktree .beans/ dirs and merges changes into
// runtime state, so the UI stays up-to-date without writing to main.
func resolveBeansPath(flagPath string, c *config.Config) (string, error) {
	explicitOverride := flagPath != "" || os.Getenv("BEANS_PATH") != ""

	var root string
	if flagPath != "" {
		root = flagPath
	} else if envPath := os.Getenv("BEANS_PATH"); envPath != "" {
		root = envPath
	} else {
		root = c.ResolveBeansPath()
	}

	if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
		if explicitOverride {
			return "", fmt.Errorf("beans path does not exist or is not a directory: %s", root)
		}
		return "", fmt.Errorf("no .beans directory found at %s (run 'beans init' to create one)", root)
	}

	return root, nil
}

// Execute runs the given root command and exits on error.
func Execute(rootCmd *cobra.Command) {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
