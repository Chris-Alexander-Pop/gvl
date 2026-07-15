package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
	"github.com/spf13/cobra"
)

func jsonMarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

var (
	modeSpeed     float64
	modeInterval  float64
	modeBright    int
	modeMinBright int
)

var modeCmd = &cobra.Command{
	Use:   "mode [name]",
	Short: "Start an animated lighting mode",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 || args[0] == "list" {
			for _, name := range mode.Names() {
				fmt.Printf("%-12s %s\n", name, mode.Help[name])
			}
			return nil
		}
		cfg := mode.Config{
			Name:          args[0],
			Speed:         modeSpeed,
			Interval:      time.Duration(modeInterval * float64(time.Second)),
			Brightness:    modeBright,
			MinBrightness: modeMinBright,
		}
		return startMode(cfg, args)
	},
}

func init() {
	modeCmd.PersistentFlags().Float64Var(&modeSpeed, "speed", 1.0, "animation speed multiplier")
	modeCmd.PersistentFlags().Float64Var(&modeInterval, "interval", 0.04, "seconds between frames")
	modeCmd.PersistentFlags().IntVarP(&modeBright, "brightness", "b", 100, "peak brightness 0-100")
	modeCmd.PersistentFlags().IntVar(&modeMinBright, "min-brightness", 15, "brightness floor")

	fadeCmd := &cobra.Command{
		Use:   "fade COLOR COLOR",
		Short: mode.Help["fade"],
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := govee.ParseColor(args[0])
			if err != nil {
				return err
			}
			b, err := govee.ParseColor(args[1])
			if err != nil {
				return err
			}
			cfg := baseModeCfg("fade")
			cfg.ColorA = &a
			cfg.ColorB = &b
			return startMode(cfg, nil)
		},
	}
	breatheCmd := &cobra.Command{
		Use:   "breathe [COLOR]",
		Short: mode.Help["breathe"],
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := baseModeCfg("breathe")
			if len(args) == 1 {
				c, err := govee.ParseColor(args[0])
				if err != nil {
					return err
				}
				cfg.Color = &c
			}
			return startMode(cfg, nil)
		},
	}
	pulseCmd := &cobra.Command{
		Use:   "pulse [COLOR]",
		Short: mode.Help["pulse"],
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := baseModeCfg("pulse")
			if len(args) == 1 {
				c, err := govee.ParseColor(args[0])
				if err != nil {
					return err
				}
				cfg.Color = &c
			}
			return startMode(cfg, nil)
		},
	}
	tempFadeCmd := &cobra.Command{
		Use:   "temp-fade TEMP TEMP",
		Short: mode.Help["temp-fade"],
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := govee.ParseTemp(args[0])
			if err != nil {
				return err
			}
			b, err := govee.ParseTemp(args[1])
			if err != nil {
				return err
			}
			cfg := baseModeCfg("temp-fade")
			cfg.TempA = a
			cfg.TempB = b
			return startMode(cfg, nil)
		},
	}
	tempCycleCmd := &cobra.Command{
		Use:   "temp-cycle",
		Short: mode.Help["temp-cycle"],
		RunE: func(cmd *cobra.Command, args []string) error {
			low, _ := cmd.Flags().GetString("low")
			high, _ := cmd.Flags().GetString("high")
			cfg := baseModeCfg("temp-cycle")
			if low != "" {
				v, err := govee.ParseTemp(low)
				if err != nil {
					return err
				}
				cfg.Low = v
			}
			if high != "" {
				v, err := govee.ParseTemp(high)
				if err != nil {
					return err
				}
				cfg.High = v
			}
			return startMode(cfg, nil)
		},
	}
	tempCycleCmd.Flags().String("low", "warm", "low temperature")
	tempCycleCmd.Flags().String("high", "cool", "high temperature")

	blendCmd := &cobra.Command{
		Use:   "blend COLOR TEMP",
		Short: mode.Help["blend"],
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := govee.ParseColor(args[0])
			if err != nil {
				return err
			}
			t, err := govee.ParseTemp(args[1])
			if err != nil {
				return err
			}
			cfg := baseModeCfg("blend")
			cfg.Color = &c
			cfg.Temp = t
			return startMode(cfg, nil)
		},
	}

	for _, name := range []string{"rainbow", "cycle", "aurora", "fire", "candle"} {
		n := name
		modeCmd.AddCommand(&cobra.Command{
			Use:   n,
			Short: mode.Help[n],
			RunE: func(cmd *cobra.Command, args []string) error {
				return startMode(baseModeCfg(n), nil)
			},
		})
	}
	modeCmd.AddCommand(fadeCmd, breatheCmd, pulseCmd, tempFadeCmd, tempCycleCmd, blendCmd)
	modeCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List modes",
		Run: func(cmd *cobra.Command, args []string) {
			for _, name := range mode.Names() {
				fmt.Printf("%-12s %s\n", name, mode.Help[name])
			}
		},
	})
}

func baseModeCfg(name string) mode.Config {
	return mode.Config{
		Name:          name,
		Speed:         modeSpeed,
		Interval:      time.Duration(modeInterval * float64(time.Second)),
		Brightness:    modeBright,
		MinBrightness: modeMinBright,
	}
}

func startMode(cfg mode.Config, _ []string) error {
	if useDaemon() {
		if err := api().StartMode(cfg); err != nil {
			return err
		}
		if !flagQuiet {
			fmt.Printf("mode %s started on daemon\n", cfg.Name)
		}
		return nil
	}
	c := deviceClient()
	if c.IP == "" {
		return fmt.Errorf("no device IP — run gvl discover or set --address")
	}
	if localRunner == nil {
		localRunner = mode.NewRunner(c)
	}
	localRunner.StartMode(cfg)
	if !flagQuiet {
		fmt.Printf("mode %s running (Ctrl+C to stop)\n", cfg.Name)
	}
	// Block until interrupted so the process keeps the goroutine alive.
	ch := make(chan os.Signal, 1)
	notifyInterrupt(ch)
	<-ch
	localRunner.Stop()
	return nil
}

func parseIntArg(s string) (int, error) {
	return strconv.Atoi(s)
}
