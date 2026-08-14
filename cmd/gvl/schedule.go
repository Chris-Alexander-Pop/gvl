package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/config"
	"github.com/Chris-Alexander-Pop/gvl/internal/schedule"
	"github.com/Chris-Alexander-Pop/gvl/internal/wizard"
	"github.com/spf13/cobra"
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Manage sleep/wake schedules on the daemon",
	Long: `Schedule commands require a configured daemon (gvl config set-url …).

  gvl schedule wizard          interactive setup
  gvl schedule list
  gvl schedule show ID
  gvl schedule enable ID
  gvl schedule disable ID
  gvl schedule delete ID
  gvl schedule run-now ID
  gvl schedule skip ID [--count N]
  gvl schedule next ID --at 09:30 [--count N]
  gvl schedule next ID --clear
`,
}

func init() {
	addQuickFlags(schedSetWakeCmd, true)
	addQuickFlags(schedSetSleepCmd, false)
	scheduleCmd.AddCommand(
		schedWizardCmd, schedListCmd, schedShowCmd,
		schedEnableCmd, schedDisableCmd, schedDeleteCmd, schedRunCmd,
		schedSetWakeCmd, schedSetSleepCmd,
		schedSkipCmd, schedNextCmd, schedUpcomingCmd,
	)
	configCmd.AddCommand(configShowCmd, configSetURLCmd, configSetTokenCmd, configSetAddrCmd)

	for _, c := range []*cobra.Command{schedSetWakeCmd, schedSetSleepCmd} {
		_ = c.RegisterFlagCompletionFunc("from-color", completeColorFlag)
		_ = c.RegisterFlagCompletionFunc("to-color", completeColorFlag)
		_ = c.RegisterFlagCompletionFunc("from-temp", completeTempFlag)
		_ = c.RegisterFlagCompletionFunc("to-temp", completeTempFlag)
		_ = c.RegisterFlagCompletionFunc("days", completeDaysFlag)
	}
}

func requireAPI() error {
	if !useDaemon() {
		return fmt.Errorf("no daemon URL configured — run: gvl config set-url http://your-gvld-host")
	}
	return nil
}

var schedWizardCmd = &cobra.Command{
	Use:   "wizard",
	Short: "Interactive helper to create a wake/sleep schedule",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAPI(); err != nil {
			return err
		}
		tz := os.Getenv("GVL_TZ")
		if tz == "" {
			tz = "UTC"
		}
		entry, err := wizard.Run(tz)
		if err != nil {
			return err
		}
		if err := api().PutSchedule(entry); err != nil {
			return err
		}
		fmt.Printf("saved schedule %q\n", entry.ID)
		return nil
	},
}

var schedListCmd = &cobra.Command{
	Use:   "list",
	Short: "List schedules",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAPI(); err != nil {
			return err
		}
		list, err := api().ListSchedules()
		if err != nil {
			return err
		}
		if flagJSON {
			now := time.Now()
			out := make([]schedule.Entry, len(list))
			for i, e := range list {
				out[i] = schedule.Decorate(e, now)
			}
			fmt.Println(mustJSON(out))
			return nil
		}
		if len(list) == 0 {
			fmt.Println("no schedules")
			return nil
		}
		fmt.Printf("%-20s %-6s %-5s %-12s %-10s %s\n", "ID", "KIND", "ON", "AT", "NEXT", "DAYS")
		now := time.Now()
		for _, e := range list {
			on := "no"
			if e.Enabled {
				on = "yes"
			}
			days := "everyday"
			if len(e.Days) > 0 {
				days = strings.Join(e.Days, ",")
			}
			next := "—"
			if when, note, ok := schedule.NextFire(e, now); ok {
				next = when.Format("Mon 15:04")
				if note != "" {
					next += "*"
				}
			}
			fmt.Printf("%-20s %-6s %-5s %-12s %-10s %s\n", e.ID, e.Kind, on, e.At+" "+e.Timezone, next, days)
		}
		return nil
	},
}

var schedShowCmd = &cobra.Command{
	Use:   "show ID",
	Short: "Show one schedule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAPI(); err != nil {
			return err
		}
		e, err := api().GetSchedule(args[0])
		if err != nil {
			return err
		}
		fmt.Println(mustJSON(schedule.Decorate(e, time.Now())))
		return nil
	},
}

var schedEnableCmd = &cobra.Command{
	Use:   "enable ID",
	Short: "Enable a schedule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAPI(); err != nil {
			return err
		}
		return api().SetScheduleEnabled(args[0], true)
	},
}

var schedDisableCmd = &cobra.Command{
	Use:   "disable ID",
	Short: "Disable a schedule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAPI(); err != nil {
			return err
		}
		return api().SetScheduleEnabled(args[0], false)
	},
}

var schedDeleteCmd = &cobra.Command{
	Use:   "delete ID",
	Short: "Delete a schedule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAPI(); err != nil {
			return err
		}
		return api().DeleteSchedule(args[0])
	},
}

var schedRunCmd = &cobra.Command{
	Use:   "run-now ID",
	Short: "Fire a schedule immediately",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAPI(); err != nil {
			return err
		}
		if err := api().RunSchedule(args[0]); err != nil {
			return err
		}
		if !flagQuiet {
			fmt.Printf("running %s\n", args[0])
		}
		return nil
	},
}

var (
	setDur      int
	setDays     string
	setTZ       string
	setFromCol  string
	setFromTemp string
	setFromB    int
	setToCol    string
	setToTemp   string
	setToB      int
	setEndOff   bool
	setID       string
)

func addQuickFlags(c *cobra.Command, wake bool) {
	c.Flags().IntVar(&setDur, "duration", 30, "ramp duration minutes")
	c.Flags().StringVar(&setDays, "days", "weekdays", "weekdays|weekend|everyday|mon,tue,...")
	c.Flags().StringVar(&setTZ, "tz", "UTC", "IANA timezone")
	c.Flags().StringVar(&setID, "id", "", "schedule id")
	c.Flags().StringVar(&setFromCol, "from-color", "", "start color")
	c.Flags().StringVar(&setFromTemp, "from-temp", "", "start temperature")
	c.Flags().IntVar(&setFromB, "from-brightness", -1, "start brightness")
	c.Flags().StringVar(&setToCol, "to-color", "", "end color")
	c.Flags().StringVar(&setToTemp, "to-temp", "", "end temperature")
	c.Flags().IntVar(&setToB, "to-brightness", -1, "end brightness")
	if wake {
		_ = c.Flags().Set("from-color", "blue")
		// defaults applied in upsertQuick when empty; wake presets:
	} else {
		c.Flags().BoolVar(&setEndOff, "end-off", true, "turn off when sleep ramp finishes")
	}
}

var schedSetWakeCmd = &cobra.Command{
	Use:   "set-wake HH:MM",
	Short: "Create/update a wake schedule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return upsertQuick(schedule.KindWake, args[0])
	},
}

var schedSetSleepCmd = &cobra.Command{
	Use:   "set-sleep HH:MM",
	Short: "Create/update a sleep schedule",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return upsertQuick(schedule.KindSleep, args[0])
	},
}

func upsertQuick(kind schedule.Kind, at string) error {
	if err := requireAPI(); err != nil {
		return err
	}
	if _, err := time.Parse("15:04", at); err != nil {
		return fmt.Errorf("invalid time %q", at)
	}
	days, err := schedule.ParseDays(setDays)
	if err != nil {
		return err
	}
	fromDef, toDef := schedule.DefaultWake()
	if kind == schedule.KindSleep {
		fromDef, toDef = schedule.DefaultSleep()
	}
	fromColor, fromTemp := setFromCol, setFromTemp
	toColor, toTemp := setToCol, setToTemp
	fromB, toB := setFromB, setToB
	if fromB < 0 {
		fromB = fromDef.Brightness
	}
	if toB < 0 {
		toB = toDef.Brightness
	}
	if kind == schedule.KindWake && fromColor == "" && fromTemp == "" {
		fromColor = "blue"
	}
	if kind == schedule.KindWake && toColor == "" && toTemp == "" {
		toTemp = "daylight"
	}
	if fromColor == "" && fromTemp == "" {
		if fromDef.Temp > 0 {
			fromTemp = strconv.Itoa(fromDef.Temp)
		} else if fromDef.Color != nil {
			fromColor = fmt.Sprintf("%d,%d,%d", fromDef.Color.R, fromDef.Color.G, fromDef.Color.B)
		}
	}
	if toColor == "" && toTemp == "" {
		if toDef.Temp > 0 {
			toTemp = strconv.Itoa(toDef.Temp)
		} else if toDef.Color != nil {
			toColor = fmt.Sprintf("%d,%d,%d", toDef.Color.R, toDef.Color.G, toDef.Color.B)
		}
	}
	from, err := schedule.ParseLook(fromColor, fromTemp, fromB)
	if err != nil {
		return fmt.Errorf("from: %w", err)
	}
	to, err := schedule.ParseLook(toColor, toTemp, toB)
	if err != nil {
		return fmt.Errorf("to: %w", err)
	}
	id := setID
	if id == "" {
		id = string(kind) + "-" + strings.ReplaceAll(at, ":", "")
	}
	var keep []schedule.Patch
	if existing, err := api().GetSchedule(id); err == nil {
		keep = existing.Next
	}
	entry := schedule.Entry{
		ID:          id,
		Enabled:     true,
		Kind:        kind,
		Days:        days,
		At:          at,
		Timezone:    setTZ,
		DurationMin: setDur,
		From:        from,
		To:          to,
		EndOff:      kind == schedule.KindSleep && setEndOff,
		Next:        keep,
	}
	if err := api().PutSchedule(entry); err != nil {
		return err
	}
	fmt.Printf("saved %s\n", id)
	return nil
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show or set local CLI config",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print config path and values",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := config.Load()
		if err != nil {
			return err
		}
		fmt.Printf("path:    %s\n", config.Path())
		fmt.Printf("url:     %s\n", c.URL)
		tok := "(empty)"
		if c.Token != "" {
			tok = "***"
		}
		fmt.Printf("token:   %s\n", tok)
		fmt.Printf("address: %s\n", c.Address)
		return nil
	},
}

var configSetURLCmd = &cobra.Command{
	Use:   "set-url URL",
	Short: "Set daemon base URL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _ := config.Load()
		c.URL = strings.TrimRight(args[0], "/")
		return config.Save(c)
	},
}

var configSetTokenCmd = &cobra.Command{
	Use:   "set-token TOKEN",
	Short: "Set daemon bearer token",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _ := config.Load()
		c.Token = args[0]
		return config.Save(c)
	},
}

var configSetAddrCmd = &cobra.Command{
	Use:   "set-address IP",
	Short: "Set default device IP for direct LAN",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _ := config.Load()
		c.Address = args[0]
		return config.Save(c)
	},
}
