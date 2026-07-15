package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/client"
	"github.com/Chris-Alexander-Pop/gvl/internal/config"
	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
	"github.com/Chris-Alexander-Pop/gvl/internal/mode"
	"github.com/spf13/cobra"
)

var (
	flagAddress string
	flagQuiet   bool
	flagJSON    bool
	flagURL     string
	flagToken   string
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
	rootCmd.PersistentFlags().StringVar(&flagURL, "url", "", "daemon base URL (overrides config)")
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "daemon bearer token")

	rootCmd.AddCommand(
		discoverCmd, statusCmd, presetsCmd, onCmd, offCmd, stopCmd,
		brightCmd, brightnessCmd, colorCmd, tempCmd, modeCmd,
		scheduleCmd, configCmd, completionCmd,
	)
}

func loadConfig() {
	var err error
	cfg, err = config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
	}
	if flagURL != "" {
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
	Long: `gvl controls Govee LAN lights and the optional gvld schedule daemon.

Direct LAN (no daemon):
  gvl discover
  gvl on
  gvl color blue
  gvl temp warm
  gvl mode rainbow

Daemon schedules (set url/token via gvl config or GVL_URL / GVL_TOKEN):
  gvl schedule wizard
  gvl schedule list
  gvl schedule run-now weekday-wake`,
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
	fmt.Println(govee.FormatStatus(st, ip))
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
	Use:   "bright [0-100]",
	Short: "Alias for brightness",
	Args:  cobra.ExactArgs(1),
	RunE:  brightnessRun,
}

var brightnessCmd = &cobra.Command{
	Use:   "brightness [0-100]",
	Short: "Set brightness 0-100",
	Args:  cobra.ExactArgs(1),
	RunE:  brightnessRun,
}

func brightnessRun(cmd *cobra.Command, args []string) error {
	var v int
	if _, err := fmt.Sscanf(args[0], "%d", &v); err != nil || v < 0 || v > 100 {
		return fmt.Errorf("brightness must be 0-100")
	}
	return simpleDevice("brightness", map[string]any{"value": v})
}

var colorCmd = &cobra.Command{
	Use:   "color [name|#hex|r,g,b]",
	Short: "Set color",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rgb, err := govee.ParseColor(args[0])
		if err != nil {
			return err
		}
		return simpleDevice("color", map[string]any{"color": rgb})
	},
}

var tempCmd = &cobra.Command{
	Use:   "temp [preset|kelvin]",
	Short: "Set color temperature",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		k, err := govee.ParseTemp(args[0])
		if err != nil {
			return err
		}
		return simpleDevice("temp", map[string]any{"value": k})
	},
}

func simpleDevice(cmdName string, payload map[string]any) error {
	if useDaemon() {
		st, err := api().DeviceCmd(cmdName, payload)
		if err != nil {
			return err
		}
		emitStatus(st, "")
		return nil
	}
	c := deviceClient()
	if c.IP == "" {
		return fmt.Errorf("no device IP — run gvl discover or set --address / GVL_DEVICE_IP")
	}
	localModeStop()
	var err error
	switch cmdName {
	case "on":
		err = c.Turn(true)
	case "off":
		err = c.Turn(false)
	case "brightness":
		v, _ := payload["value"].(int)
		err = c.Brightness(v)
	case "color":
		rgb, _ := payload["color"].(govee.RGB)
		err = c.Color(rgb)
	case "temp":
		v, _ := payload["value"].(int)
		err = c.Temp(v)
	}
	if err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	st, err := c.Status(2 * time.Second)
	if err != nil {
		if !flagQuiet {
			fmt.Fprintf(os.Stderr, "sent command to %s, but got no status reply\n", c.IP)
		}
		return err
	}
	emitStatus(st, c.IP)
	return nil
}

// localRunner is used for direct-LAN animated modes.
var localRunner *mode.Runner

func localModeStop() {
	if localRunner != nil {
		localRunner.Stop()
	}
}

var presetsCmd = &cobra.Command{
	Use:   "presets",
	Short: "List colors, temperatures, modes",
	Run: func(cmd *cobra.Command, args []string) {
		if flagJSON {
			fmt.Println(mustJSON(map[string]any{
				"colors": govee.NamedColors,
				"temps":  govee.TempPresets,
				"modes":  mode.Help,
			}))
			return
		}
		fmt.Println("colors")
		for _, name := range sortedMapKeys(govee.NamedColors) {
			c := govee.NamedColors[name]
			fmt.Printf("  %-12s #%02x%02x%02x\n", name, c.R, c.G, c.B)
		}
		fmt.Println("\ntemperatures")
		for _, name := range sortedMapKeys(govee.TempPresets) {
			fmt.Printf("  %-12s %dK\n", name, govee.TempPresets[name])
		}
		fmt.Println("\nmodes")
		for _, name := range mode.Names() {
			fmt.Printf("  %-12s %s\n", name, mode.Help[name])
		}
	},
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
