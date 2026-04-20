// Package main is the entry point for the enveil CLI.
package main

import (
	"fmt"
	"os"

	"github.com/leonzalion/enveil/internal/run"
	"github.com/leonzalion/enveil/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	defaultStorePath  = ""   // resolved at runtime from $HOME/.enveil
	agentInternalFlag = "_ENVEIL_AGENT_INTERNAL"
)

func storePath() string {
	home, _ := os.UserHomeDir()
	return home + "/.enveil"
}

func main() {
	// If re-executed as the background agent process, run the agent loop.
	if os.Getenv(agentInternalFlag) == "1" {
		runAgentInternal()
		return
	}

	root := &cobra.Command{
		Use:   "enveil",
		Short: "Secure secret management for developer environments",
	}

	root.AddCommand(agentCmd(), runCmd(), secretCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// ── agent commands ────────────────────────────────────────────────────────────

func agentCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Manage the enveil agent"}
	cmd.AddCommand(agentStartCmd(), agentStopCmd(), agentStatusCmd())
	return cmd
}

func agentStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the enveil agent (prompts for master password)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return agentStart()
		},
	}
}

func agentStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running enveil agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			return agentStop()
		},
	}
}

func agentStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check if the enveil agent is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			return agentStatus()
		},
	}
}

// ── run command ───────────────────────────────────────────────────────────────

func runCmd() *cobra.Command {
	var envFile string
	cmd := &cobra.Command{
		Use:                "run [--env .env] -- <cmd> [args...]",
		Short:              "Run a command with secrets injected from the agent",
		DisableFlagParsing: false,
		Args:               cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: enveil run [--env .env] -- <cmd> [args...]")
			}
			binary, err := lookPath(args[0])
			if err != nil {
				return fmt.Errorf("command not found: %s", args[0])
			}
			return run.Run(envFile, binary, args[1:])
		},
	}
	cmd.Flags().StringVar(&envFile, "env", ".env", "path to .env file")
	return cmd
}

// ── secret commands ───────────────────────────────────────────────────────────

func secretCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "secret", Short: "Manage secrets in the encrypted store"}
	cmd.AddCommand(
		secretAddCmd(),
		secretListCmd(),
		secretDeleteCmd(),
		secretRotateCmd(),
	)
	return cmd
}

func secretAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <item> <field>",
		Short: "Add a secret to the store (prompts for value)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, field := args[0], args[1]
			password, err := promptPassword("Master password: ")
			if err != nil {
				return err
			}
			value, err := promptPassword(fmt.Sprintf("Value for %s/%s: ", item, field))
			if err != nil {
				return err
			}

			sp := storePath()
			var s *store.Store
			if _, err := os.Stat(sp); os.IsNotExist(err) {
				s, err = store.Init(sp, password)
				if err != nil {
					return fmt.Errorf("initialising store: %w", err)
				}
			} else {
				s, err = store.Open(sp, password)
				if err != nil {
					return err
				}
			}

			s.Add(item, field, string(value))
			if err := s.Save(); err != nil {
				return fmt.Errorf("saving store: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Added %s/%s\n", item, field)
			return nil
		},
	}
}

func secretListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all secret keys (item/field) in the store",
		RunE: func(cmd *cobra.Command, args []string) error {
			password, err := promptPassword("Master password: ")
			if err != nil {
				return err
			}
			s, err := store.Open(storePath(), password)
			if err != nil {
				return err
			}
			for _, k := range s.List() {
				fmt.Println(k)
			}
			return nil
		},
	}
}

func secretDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <item> <field>",
		Short: "Delete a secret from the store",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, field := args[0], args[1]
			password, err := promptPassword("Master password: ")
			if err != nil {
				return err
			}
			s, err := store.Open(storePath(), password)
			if err != nil {
				return err
			}
			if !s.Delete(item, field) {
				return fmt.Errorf("secret %s/%s not found", item, field)
			}
			if err := s.Save(); err != nil {
				return fmt.Errorf("saving store: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Deleted %s/%s\n", item, field)
			return nil
		},
	}
}

func secretRotateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate <item> <field>",
		Short: "Update a secret value (re-prompts and re-encrypts)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, field := args[0], args[1]
			password, err := promptPassword("Master password: ")
			if err != nil {
				return err
			}
			s, err := store.Open(storePath(), password)
			if err != nil {
				return err
			}
			newVal, err := promptPassword(fmt.Sprintf("New value for %s/%s: ", item, field))
			if err != nil {
				return err
			}
			s.Add(item, field, string(newVal))
			if err := s.Save(); err != nil {
				return fmt.Errorf("saving store: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Rotated %s/%s\n", item, field)
			return nil
		},
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func promptPassword(prompt string) ([]byte, error) {
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return pw, err
}
