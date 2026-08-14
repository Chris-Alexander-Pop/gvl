package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/client"
	"github.com/Chris-Alexander-Pop/gvl/internal/config"
	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
	"github.com/Chris-Alexander-Pop/gvl/internal/trace"
	"github.com/Chris-Alexander-Pop/gvl/internal/ui"
	"github.com/spf13/cobra"
)

var (
	flagAddress string
	flagQuiet   bool
	flagJSON    bool
	flagURL     string
	flagToken   string
	flagVerbose bool
	cfg         config.Config
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(loadConfig)
	rootCmd.PersistentFlags().StringVarP(&flagAddress, "address", "a", "", "device IP (direct LAN)")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "no output")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "print JSON")
	rootCmd.PersistentFlags().StringVar(&flagURL, "url", "", "daemon base URL (overrides config; \"local\" = direct LAN)")
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "daemon bearer token")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "log UDP/HTTP timings to stderr (or set GVL_DEBUG=1)")

	rootCmd.AddCommand(
		discoverCmd, crawlCmd, statusCmd, presetsCmd, onCmd, offCmd, stopCmd,
		brightCmd, brightnessCmd, colorCmd, colorUSCmd, setCmd, tempCmd, modeCmd,
		scheduleCmd, configCmd, completionCmd,
	)

	// Hidden exact-match aliases — work when typed, never clutter tab/help.
	brightnessCmd.Hidden = true
	colorUSCmd.Hidden = true

	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		if c == rootCmd {
			fmt.Println(ui.FormatHome())
			return
		}
		defaultHelp(c, args)
	})
}

func loadConfig() {
	trace.InitFromEnv()
	if flagVerbose {
		trace.Enable()
	}
	var err error
	cfg, err = config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
	}
	switch flagURL {
	case "":
		// keep config / env
	case "local", "-":
		cfg.URL = "" // force direct LAN (ignore daemon)
	default:
		cfg.URL = flagURL
	}
	if flagToken != "" {
		cfg.Token = flagToken
	}
	if flagAddress == "" {
		flagAddress = cfg.Address
	}
	if flagAddress == "" {
		flagAddress = govee.DefaultDeviceIP()
	}
}

var rootCmd = &cobra.Command{
	Use:   "gvl",
	Short: "Control Govee lights over LAN (and schedules via gvld)",
	Long:  "Control Govee LAN lights and the optional gvld schedule daemon.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(ui.FormatHome())
	},
}

func api() *client.Client {
	if cfg.URL == "" {
		return nil
	}
	return client.New(cfg.URL, cfg.Token)
}

func useDaemon() bool {
	return cfg.URL != ""
}

func deviceClient() *govee.Client {
	return govee.NewClient(flagAddress)
}

func emitStatus(st *govee.Status, ip string) {
	if flagQuiet {
		return
	}
	if flagJSON {
		fmt.Printf("%s\n", mustJSON(st))
		return
	}
	fmt.Println(ui.FormatStatus(st, ip))
}

func mustJSON(v any) string {
	b, _ := jsonMarshalIndent(v)
	return string(b)
}

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Find LAN-enabled devices",
	RunE: func(cmd *cobra.Command, args []string) error {
		if useDaemon() {
			devs, err := api().Discover()
			if err != nil {
				return err
			}
			return printDevices(devs)
		}
		devs, err := govee.Discover(3 * time.Second)
		if err != nil {
			return err
		}
		if len(devs) == 0 {
			return fmt.Errorf("no devices found. Is LAN control enabled?")
		}
		_ = govee.CacheIPs(devs)
		return printDevices(devs)
	},
}

func printDevices(devs []govee.Device) error {
	if flagJSON {
		fmt.Println(mustJSON(devs))
		return nil
	}
	for _, d := range devs {
		sku := d.SKU
		if sku == "" {
			sku = "?"
		}
		fmt.Printf("%-8s %-15s %s\n", sku, d.IP, d.Device)
	}
	return nil
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current state",
	RunE: func(cmd *cobra.Command, args []string) error {
		if useDaemon() {
			st, ip, err := api().Status()
			if err != nil {
				return err
			}
			emitStatus(st, ip)
			return nil
		}
		st, err := deviceClient().Status(2 * time.Second)
		if err != nil {
			return err
		}
		emitStatus(st, flagAddress)
		return nil
	},
}

var onCmd = &cobra.Command{
	Use:   "on",
	Short: "Turn on",
	RunE:  func(cmd *cobra.Command, args []string) error { return simpleDevice("on", nil) },
}

var offCmd = &cobra.Command{
	Use:   "off",
	Short: "Turn off",
	RunE:  func(cmd *cobra.Command, args []string) error { return simpleDevice("off", nil) },
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop background animated mode / ramp",
	RunE: func(cmd *cobra.Command, args []string) error {
		if useDaemon() {
			if err := api().Stop(); err != nil {
				return err
			}
			if !flagQuiet {
				fmt.Println("stopped")
			}
			return nil
		}
		if !flagQuiet {
			fmt.Println("no daemon configured; local modes are process-local — start modes via gvld for stop support")
		}
		return nil
	},
}

var brightCmd = &cobra.Command{
	Use:   "bright [0-100] [setting value...]",
	Short: "Set brightness 0–100",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return applyLeading("bright", args)
	},
}

// brightnessCmd is a hidden exact-match alias for bright (not shown in tab/help).
var brightnessCmd = &cobra.Command{
	Use:   "brightness [0-100] [setting value...]",
	Short: "Set brightness 0–100",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return applyLeading("bright", args)
	},
}

var colorCmd = &cobra.Command{
	Use:   "colour [name|#hex|r,g,b] [setting value...]",
	Short: "Set colour",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return applyLeading("color", args)
	},
}

// colorUSCmd is a hidden exact-match alias for colour.
var colorUSCmd = &cobra.Command{
	Use:   "color [name|#hex|r,g,b] [setting value...]",
	Short: "Set colour",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return applyLeading("color", args)
	},
}

var setCmd = &cobra.Command{
	Use:   "set [setting value...]",
	Short: "Apply several settings in one go",
	Long: `Apply chained light settings. Keys: colour, bright, temp, on, off.

  gvl set colour red bright 40
  gvl set on temp warm bright 25
  gvl set colour #ff8800 bright 60`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return applySettings(args)
	},
}

var tempCmd = &cobra.Command{
	Use:   "temp [preset|kelvin] [setting value...]",
	Short: "Set colour temperature",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return applyLeading("temp", args)
	},
}

func simpleDevice(cmdName string, payload map[string]any) error {
	st, ip, err := deviceCmd(cmdName, payload, true)
	if err != nil {
		return err
	}
	emitStatus(st, ip)
	return nil
}

// deviceCmd sends one device command. When wantStatus is false (local LAN only),
// skips the status poll — used for intermediate steps in a chain.
func deviceCmd(cmdName string, payload map[string]any, wantStatus bool) (*govee.Status, string, error) {
	t0 := time.Now()
	defer func() {
		via := "lan"
		if useDaemon() {
			via = "gvld"
		}
		trace.Printf("cmd %s via=%s %s", cmdName, via, time.Since(t0).Round(time.Millisecond))
	}()
	if useDaemon() {
		st, err := api().DeviceCmd(cmdName, payload)
		if err != nil {
			return nil, "", err
		}
		return st, "", nil
	}
	c := deviceClient()
	if c.IP == "" {
		return nil, "", fmt.Errorf("no device IP — run gvl discover or set --address / GVL_DEVICE_IP")
	}
	localModeStop()
	if !wantStatus {
		var err error
		switch cmdName {
		case "on":
			err = c.PushTurn(true)
		case "off":
			err = c.PushTurn(false)
		case "brightness":
			v, _ := payload["value"].(int)
			err = c.PushBrightness(v)
		case "color":
			rgb, _ := payload["color"].(govee.RGB)
			err = c.PushColor(rgb)
		case "temp":
			v, _ := payload["value"].(int)
			err = c.PushTemp(v)
		default:
			err = fmt.Errorf("unknown command %q", cmdName)
		}
		return nil, c.IP, err
	}
	var (
		st  *govee.Status
		err error
	)
	switch cmdName {
	case "on":
		st, err = c.ExecTurn(true)
	case "off":
		st, err = c.ExecTurn(false)
	case "brightness":
		v, _ := payload["value"].(int)
		st, err = c.ExecBrightness(v)
	case "color":
		rgb, _ := payload["color"].(govee.RGB)
		st, err = c.ExecColor(rgb)
	case "temp":
		v, _ := payload["value"].(int)
		st, err = c.ExecTemp(v)
	default:
		err = fmt.Errorf("unknown command %q", cmdName)
	}
	if err != nil {
		return nil, "", err
	}
	return st, c.IP, nil
}

// localRunner is used for direct-LAN animated modes.
var localRunner *mode.Runner

func localModeStop() {
	if localRunner != nil {
		localRunner.Stop()
	}
}

var presetsCmd = &cobra.Command{
	Use:     "presets",
	Short:   "List colours, temperatures, modes (with swatches)",
	Aliases: []string{"colours", "colors", "aliases"},
	Run: func(cmd *cobra.Command, args []string) {
		if flagJSON {
			fmt.Println(mustJSON(map[string]any{
				"colors": govee.NamedColors,
				"temps":  govee.TempPresets,
				"modes":  mode.Help,
			}))
			return
		}
		fmt.Println(ui.FormatPresets())
	},
}

