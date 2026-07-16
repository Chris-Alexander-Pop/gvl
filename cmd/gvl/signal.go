package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

func notifyInterrupt(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish]",
	Short: "Generate or install shell completion",
	Long: `Generate shell completion scripts, or install them for your user account.

  gvl completion install zsh     # oh-my-zsh custom completions (recommended)
  gvl completion zsh             # print script to stdout
  gvl completion bash > …`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if args[0] == "install" {
			shell := "zsh"
			if len(args) == 2 {
				shell = args[1]
			}
			return installCompletion(shell)
		}
		if len(args) != 1 {
			return fmt.Errorf("usage: gvl completion [bash|zsh|fish]  or  gvl completion install [zsh|bash|fish]")
		}
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		default:
			return fmt.Errorf("unsupported shell %q (want bash, zsh, fish, or: completion install zsh)", args[0])
		}
	},
}
