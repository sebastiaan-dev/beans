package commands

import (
	_ "embed"
	"os"
	"text/template"

	"github.com/hmans/beans/pkg/config"
	"github.com/spf13/cobra"
)

//go:embed prompt.tmpl
var agentPromptTemplate string

// promptData holds all data needed to render the prompt template.
type promptData struct {
	GraphQLSchema string
	Types         []config.TypeConfig
	Statuses      []config.StatusConfig
	Priorities    []config.PriorityConfig
}

var primeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Output instructions for AI coding agents",
	Long:  `Outputs a prompt that primes AI coding agents on how to use the beans CLI to manage project issues.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Only emit the primer for projects that opted in to beans: an
		// explicit --beans-path, an explicit config file (--config or
		// BEANS_CONFIG), or a .beans.yml found by searching upward.
		cwd, err := os.Getwd()
		if err != nil {
			return nil // Silently exit on error
		}
		configFile, err := resolveConfigFile(configPath, cwd)
		if err != nil {
			// An explicitly configured config file is missing. Report it
			// instead of silently priming nothing.
			cmd.SilenceUsage = true
			return err
		}
		if beansPath == "" && configFile == "" {
			// Not a beans project - silently exit
			return nil
		}

		tmpl, err := template.New("prompt").Parse(agentPromptTemplate)
		if err != nil {
			return err
		}

		data := promptData{
			GraphQLSchema: GetGraphQLSchema(),
			Types:         config.DefaultTypes,
			Statuses:      config.DefaultStatuses,
			Priorities:    config.DefaultPriorities,
		}

		return tmpl.Execute(cmd.OutOrStdout(), data)
	},
}

func RegisterPrimeCmd(root *cobra.Command) {
	root.AddCommand(primeCmd)
}
