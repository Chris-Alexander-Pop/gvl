package main

import (
	"fmt"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/schedule"
	"github.com/spf13/cobra"
)

var (
	skipCount   int
	skipDate    string
	nextAt      string
	nextDate    string
	nextCount   int
	nextSkip    bool
	nextClear   bool
	nextDay     bool
	nextDur     int
	nextFromCol string
	nextFromTmp string
	nextFromB   int
	nextToCol   string
	nextToTmp   string
	nextToB     int
	nextEndOff  bool
	nextEndSet  bool
)

func init() {
	schedSkipCmd.Flags().IntVar(&skipCount, "count", 1, "how many upcoming occurrences to skip")
	schedSkipCmd.Flags().StringVar(&skipDate, "date", "", "YYYY-MM-DD of the first occurrence to skip")

	schedNextCmd.Flags().StringVar(&nextAt, "at", "", "HH:MM for the next occurrence(s)")
	schedNextCmd.Flags().StringVar(&nextDate, "date", "", "YYYY-MM-DD of the first occurrence to patch")
	schedNextCmd.Flags().IntVar(&nextCount, "count", 1, "how many upcoming occurrences to patch")
	schedNextCmd.Flags().BoolVar(&nextSkip, "skip", false, "skip instead of moving")
	schedNextCmd.Flags().BoolVar(&nextClear, "clear", false, "remove all one-shot overrides")
	schedNextCmd.Flags().BoolVar(&nextDay, "next-day", false, "fire At on the calendar day after the occurrence (e.g. 01:00 after a 23:00 slot)")
	schedNextCmd.Flags().IntVar(&nextDur, "duration", 0, "override ramp minutes (0 = keep)")
	schedNextCmd.Flags().StringVar(&nextFromCol, "from-color", "", "override start color")
	schedNextCmd.Flags().StringVar(&nextFromTmp, "from-temp", "", "override start temperature")
	schedNextCmd.Flags().IntVar(&nextFromB, "from-brightness", -1, "override start brightness")
	schedNextCmd.Flags().StringVar(&nextToCol, "to-color", "", "override end color")
	schedNextCmd.Flags().StringVar(&nextToTmp, "to-temp", "", "override end temperature")
	schedNextCmd.Flags().IntVar(&nextToB, "to-brightness", -1, "override end brightness")
	schedNextCmd.Flags().BoolVar(&nextEndOff, "end-off", false, "override sleep end-off (use --end-off=false to keep lights on)")
	schedNextCmd.PreRun = func(cmd *cobra.Command, args []string) {
		nextEndSet = cmd.Flags().Changed("end-off")
	}

	for _, c := range []*cobra.Command{schedSkipCmd, schedNextCmd} {
		c.ValidArgsFunction = completeScheduleIDs
	}
	_ = schedNextCmd.RegisterFlagCompletionFunc("from-color", completeColorFlag)
	_ = schedNextCmd.RegisterFlagCompletionFunc("to-color", completeColorFlag)
	_ = schedNextCmd.RegisterFlagCompletionFunc("from-temp", completeTempFlag)
	_ = schedNextCmd.RegisterFlagCompletionFunc("to-temp", completeTempFlag)
}

var schedSkipCmd = &cobra.Command{
	Use:   "skip ID",
	Short: "Skip the next occurrence(s) without changing the recurring time",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAPI(); err != nil {
			return err
		}
		e, err := api().SkipNext(args[0], skipCount, skipDate)
		if err != nil {
			return err
		}
		return printPatched(e, "skipped")
	},
}

var schedNextCmd = &cobra.Command{
	Use:   "next ID",
	Short: "Override the next occurrence(s) only (time, skip, or look)",
	Long: `Change only the next fire(s). The recurring schedule stays the same.

  gvl schedule next weekday-wake --at 09:30
  gvl schedule next weekday-wake --at 09:30 --count 3
  gvl schedule next weekday-sleep --at 01:00 --next-day
  gvl schedule next weekday-wake --skip
  gvl schedule next weekday-wake --at 09:00 --duration 45 --to-temp daylight --to-brightness 40
  gvl schedule next weekday-wake --clear`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAPI(); err != nil {
			return err
		}
		id := args[0]
		if nextClear {
			e, err := api().ClearNext(id)
			if err != nil {
				return err
			}
			return printPatched(e, "cleared")
		}
		if nextSkip && nextAt != "" {
			return fmt.Errorf("use either --skip or --at, not both")
		}
		if nextSkip {
			e, err := api().SkipNext(id, nextCount, nextDate)
			if err != nil {
				return err
			}
			return printPatched(e, "skipped")
		}
		existing, err := api().GetSchedule(id)
		if err != nil {
			return err
		}
		spec := schedule.Patch{
			At:          nextAt,
			Date:        nextDate,
			NextDay:     nextDay,
			DurationMin: nextDur,
			Skip:        false,
		}
		if nextFromCol != "" || nextFromTmp != "" || nextFromB >= 0 {
			look := existing.From
			if nextFromCol != "" || nextFromTmp != "" {
				b := nextFromB
				if b < 0 {
					b = existing.From.Brightness
				}
				parsed, err := schedule.ParseLook(nextFromCol, nextFromTmp, b)
				if err != nil {
					return fmt.Errorf("from: %w", err)
				}
				look = parsed
			} else {
				look.Brightness = nextFromB
			}
			spec.From = &look
		}
		if nextToCol != "" || nextToTmp != "" || nextToB >= 0 {
			look := existing.To
			if nextToCol != "" || nextToTmp != "" {
				b := nextToB
				if b < 0 {
					b = existing.To.Brightness
				}
				parsed, err := schedule.ParseLook(nextToCol, nextToTmp, b)
				if err != nil {
					return fmt.Errorf("to: %w", err)
				}
				look = parsed
			} else {
				look.Brightness = nextToB
			}
			spec.To = &look
		}
		if nextEndSet {
			v := nextEndOff
			spec.EndOff = &v
		}
		if spec.At == "" && spec.From == nil && spec.To == nil && spec.DurationMin == 0 && spec.EndOff == nil {
			return printPatched(schedule.Decorate(existing, time.Now()), "")
		}
		e, err := api().PatchNext(id, spec, nextCount)
		if err != nil {
			return err
		}
		return printPatched(e, "patched")
	},
}

var schedUpcomingCmd = &cobra.Command{
	Use:   "upcoming",
	Short: "Show the next fire time for every schedule",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAPI(); err != nil {
			return err
		}
		list, err := api().ListSchedules()
		if err != nil {
			return err
		}
		now := time.Now()
		if flagJSON {
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
		fmt.Printf("%-20s %-6s %-22s %s\n", "ID", "KIND", "NEXT", "NOTE")
		for _, e := range list {
			e = schedule.Decorate(e, now)
			when := "—"
			if e.Upcoming != "" {
				if t, err := time.Parse(time.RFC3339, e.Upcoming); err == nil {
					when = t.Format("Mon 2006-01-02 15:04")
				} else {
					when = e.Upcoming
				}
			}
			fmt.Printf("%-20s %-6s %-22s %s\n", e.ID, e.Kind, when, e.UpcomingNote)
		}
		return nil
	},
}

func printPatched(e schedule.Entry, verb string) error {
	e = schedule.Decorate(e, time.Now())
	if flagJSON {
		fmt.Println(mustJSON(e))
		return nil
	}
	if verb != "" && !flagQuiet {
		fmt.Printf("%s %s\n", verb, e.ID)
	}
	if e.Upcoming != "" {
		when := e.Upcoming
		if t, err := time.Parse(time.RFC3339, e.Upcoming); err == nil {
			when = t.Format("Mon 15:04 MST")
		}
		fmt.Printf("next fire %s", when)
		if e.UpcomingNote != "" {
			fmt.Printf(" (%s)", e.UpcomingNote)
		}
		fmt.Println()
	} else if !e.Enabled {
		fmt.Println("disabled — nothing upcoming")
	} else {
		fmt.Println("no upcoming occurrence")
	}
	if len(e.Next) > 0 {
		fmt.Printf("%d override(s) queued\n", len(e.Next))
	}
	return nil
}
