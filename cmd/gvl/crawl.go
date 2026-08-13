package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Chris-Alexander-Pop/gvl/internal/config"
	"github.com/Chris-Alexander-Pop/gvl/internal/govee"
	"github.com/spf13/cobra"
)

var (
	crawlTimeout   time.Duration
	crawlSave      bool
	crawlMulticast bool
)

func init() {
	crawlCmd.Flags().DurationVar(&crawlTimeout, "timeout", 2*time.Second, "per-subnet probe wait")
	crawlCmd.Flags().BoolVar(&crawlSave, "save", false, "write a single found IP to config address")
	crawlCmd.Flags().BoolVar(&crawlMulticast, "multicast", true, "also try multicast/broadcast scan")
}

var crawlCmd = &cobra.Command{
	Use:   "crawl [cidr|ip...]",
	Short: "Find Govee lights by probing LAN subnets",
	Long: `Find LAN-enabled Govee lights without the daemon.

Always talks UDP on the local machine (ignores --url / config url), so it still
works when gvld is down or on another network.

With no arguments, probes:
  1. GVL_DISCOVER_SUBNET (if set)
  2. /24 of the configured/cached device IP
  3. IPv4 networks on local interfaces

Pass CIDRs or bare IPs (bare IP → that host's /24):

  gvl crawl
  gvl crawl 192.0.2.0/24
  gvl crawl 192.0.2.10 198.51.100.0/24
  gvl crawl --save`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCrawl(args)
	},
}

type crawlHit struct {
	IP     string        `json:"ip"`
	SKU    string        `json:"sku,omitempty"`
	Device string        `json:"device,omitempty"`
	Status *govee.Status `json:"status,omitempty"`
	How    string        `json:"how"`
}

func runCrawl(args []string) error {
	cidrs, err := crawlTargets(args)
	if err != nil {
		return err
	}
	if len(cidrs) == 0 {
		return fmt.Errorf("no subnets to probe — pass a CIDR (e.g. 192.0.2.0/24) or set GVL_DISCOVER_SUBNET")
	}

	byIP := map[string]*crawlHit{}

	if crawlMulticast {
		progressf("scan: multicast/broadcast…")
		devs, err := govee.Discover(3 * time.Second)
		if err == nil {
			for _, d := range devs {
				if d.IP == "" {
					continue
				}
				h := byIP[d.IP]
				if h == nil {
					h = &crawlHit{IP: d.IP, How: "scan"}
					byIP[d.IP] = h
				}
				h.SKU = d.SKU
				h.Device = d.Device
				h.How = "scan"
			}
		}
	}

	for _, cidr := range cidrs {
		progressf("probe: %s …", cidr)
		results, err := govee.ProbeSubnet(cidr, crawlTimeout)
		if err != nil {
			progressf("  skip %s: %v", cidr, err)
			continue
		}
		for _, r := range results {
			h := byIP[r.IP]
			if h == nil {
				h = &crawlHit{IP: r.IP, How: "probe"}
				byIP[r.IP] = h
			}
			h.Status = r.Status
			switch h.How {
			case "scan":
				h.How = "scan+probe"
			case "":
				h.How = "probe"
			}
		}
	}

	if len(byIP) == 0 {
		return fmt.Errorf("no devices answered on %s", strings.Join(cidrs, ", "))
	}

	hits := make([]*crawlHit, 0, len(byIP))
	for _, h := range byIP {
		hits = append(hits, h)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].IP < hits[j].IP })

	devs := make([]govee.Device, 0, len(hits))
	for _, h := range hits {
		devs = append(devs, govee.Device{IP: h.IP, SKU: h.SKU, Device: h.Device})
	}
	_ = govee.CacheIPs(devs)

	if flagJSON {
		fmt.Println(mustJSON(map[string]any{
			"cidrs":   cidrs,
			"devices": hits,
		}))
	} else if !flagQuiet {
		for _, h := range hits {
			printCrawlHit(h)
		}
		fmt.Fprintf(os.Stderr, "\nnext: gvl -a %s --url local status\n", hits[0].IP)
	}

	if crawlSave {
		probes := make([]govee.ProbeResult, len(hits))
		for i, h := range hits {
			probes[i] = govee.ProbeResult{IP: h.IP, Status: h.Status}
		}
		ip, err := govee.PickProbedIP(probes, flagAddress)
		if err != nil {
			return fmt.Errorf("save: %w", err)
		}
		c, _ := config.Load()
		c.Address = ip
		if err := config.Save(c); err != nil {
			return err
		}
		if !flagQuiet {
			fmt.Printf("saved address %s → %s\n", ip, config.Path())
		}
	}
	return nil
}

func crawlTargets(args []string) ([]string, error) {
	if len(args) > 0 {
		out := make([]string, 0, len(args))
		seen := map[string]struct{}{}
		for _, a := range args {
			c, err := govee.ParseCrawlTarget(a)
			if err != nil {
				return nil, err
			}
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			out = append(out, c)
		}
		return out, nil
	}
	return govee.DefaultCrawlCIDRs(flagAddress), nil
}

func printCrawlHit(h *crawlHit) {
	sku := h.SKU
	if sku == "" {
		sku = "?"
	}
	dev := h.Device
	if dev == "" {
		dev = "-"
	}
	fmt.Printf("%-8s %-15s %s", sku, h.IP, dev)
	if h.Status != nil {
		power := "off"
		if h.Status.OnOff != 0 {
			power = "on"
		}
		fmt.Printf("  %s %d%% %s", power, h.Status.Brightness, govee.FormatColor(h.Status.Color, h.Status.ColorTemInKelvin))
	}
	fmt.Printf("  (%s)\n", h.How)
}

func progressf(format string, args ...any) {
	if flagQuiet || flagJSON {
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
