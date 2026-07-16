package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
	"github.com/spf13/cobra"
)

func init() {
	colorCmd.ValidArgsFunction = completeColorLeading
	colorUSCmd.ValidArgsFunction = completeColorLeading
	brightCmd.ValidArgsFunction = completeBrightLeading
	brightnessCmd.ValidArgsFunction = completeBrightLeading
	tempCmd.ValidArgsFunction = completeTempLeading
	setCmd.ValidArgsFunction = completeSetArgs
	modeCmd.ValidArgsFunction = completeModeNames

	for _, c := range []*cobra.Command{schedShowCmd, schedEnableCmd, schedDisableCmd, schedDeleteCmd, schedRunCmd} {
		c.ValidArgsFunction = completeScheduleIDs
	}

	completionCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return filterPrefix([]string{"bash", "zsh", "fish", "install"}, toComplete), cobra.ShellCompDirectiveNoFileComp
		}
		if len(args) == 1 && args[0] == "install" {
			return filterPrefix([]string{"bash", "zsh", "fish"}, toComplete), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

func colorNameList() []string {
	names := make([]string, 0, len(govee.NamedColors))
	for n := range govee.NamedColors {
		names = append(names, n)
	}
	return names
}

func tempNameList() []string {
	names := make([]string, 0, len(govee.TempPresets))
	for n := range govee.TempPresets {
		names = append(names, n)
	}
	return names
}

func settingKeyList() []string {
	return []string{"colour", "bright", "temp", "on", "off"}
}

func completeColorFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterPrefix(colorNameList(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeTempFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterPrefix(tempNameList(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeDaysFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterPrefix([]string{"weekdays", "weekend", "everyday", "mon", "tue", "wed", "thu", "fri", "sat", "sun"}, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeModeNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterPrefix(append(mode.Names(), "list"), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeColorLeading(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return filterPrefix(colorNameList(), toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	return completeSettingChain(args[1:], toComplete)
}

func completeBrightLeading(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return filterPrefix([]string{"5", "10", "20", "30", "40", "50", "60", "75", "100"}, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	return completeSettingChain(args[1:], toComplete)
}

func completeTempLeading(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return filterPrefix(tempNameList(), toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	return completeSettingChain(args[1:], toComplete)
}

func completeSetArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeSettingChain(args, toComplete)
}

// completeSettingChain completes key/value pairs: colour red bright 40 …
func completeSettingChain(args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args)%2 == 0 {
		return filterPrefix(settingKeyList(), toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	key := strings.ToLower(args[len(args)-1])
	switch key {
	case "color", "colour":
		return filterPrefix(colorNameList(), toComplete), cobra.ShellCompDirectiveNoFileComp
	case "bright", "brightness":
		return filterPrefix([]string{"5", "10", "20", "30", "40", "50", "60", "75", "100"}, toComplete), cobra.ShellCompDirectiveNoFileComp
	case "temp":
		return filterPrefix(tempNameList(), toComplete), cobra.ShellCompDirectiveNoFileComp
	case "on", "off":
		// bare tokens — next should be another key; treat as even by offering keys
		return filterPrefix(settingKeyList(), toComplete), cobra.ShellCompDirectiveNoFileComp
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

func completeScheduleIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if !useDaemon() {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	list, err := api().ListSchedules()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ids := make([]string, 0, len(list))
	for _, e := range list {
		ids = append(ids, e.ID)
	}
	return filterPrefix(ids, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func filterPrefix(items []string, prefix string) []string {
	if prefix == "" {
		return items
	}
	p := strings.ToLower(prefix)
	out := make([]string, 0, len(items))
	for _, it := range items {
		if strings.HasPrefix(strings.ToLower(it), p) {
			out = append(out, it)
		}
	}
	return out
}

// installCompletion writes shell completion into standard user locations.
func installCompletion(shell string) error {
	switch shell {
	case "zsh":
		dir := os.Getenv("ZSH")
		if dir == "" {
			home, _ := os.UserHomeDir()
			dir = home + "/.oh-my-zsh"
		}
		path := dir + "/custom/completions/_gvl"
		if err := os.MkdirAll(dir+"/custom/completions", 0o755); err != nil {
			return err
		}
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := rootCmd.GenZshCompletion(f); err != nil {
			return err
		}
		fmt.Printf("installed zsh completion → %s\n", path)
		// Old govee-lan completion used to claim `gvl` too — unhook it.
		fixGovee := dir + "/custom/completions/_govee-lan"
		if b, err := os.ReadFile(fixGovee); err == nil {
			s := string(b)
			if strings.Contains(s, "#compdef govee-lan gvl") {
				s = strings.Replace(s, "#compdef govee-lan gvl", "#compdef govee-lan", 1)
				_ = os.WriteFile(fixGovee, []byte(s), 0o644)
				fmt.Println("fixed: _govee-lan no longer steals gvl tab completion")
			}
		}
		fmt.Println("restart your shell (or run: exec zsh) then try: gvl <tab>")
		return nil
	case "bash":
		home, _ := os.UserHomeDir()
		path := home + "/.local/share/bash-completion/completions/gvl"
		if err := os.MkdirAll(home+"/.local/share/bash-completion/completions", 0o755); err != nil {
			return err
		}
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := rootCmd.GenBashCompletion(f); err != nil {
			return err
		}
		fmt.Printf("installed bash completion → %s\n", path)
		return nil
	case "fish":
		home, _ := os.UserHomeDir()
		path := home + "/.config/fish/completions/gvl.fish"
		if err := os.MkdirAll(home+"/.config/fish/completions", 0o755); err != nil {
			return err
		}
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := rootCmd.GenFishCompletion(f, true); err != nil {
			return err
		}
		fmt.Printf("installed fish completion → %s\n", path)
		return nil
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
}
