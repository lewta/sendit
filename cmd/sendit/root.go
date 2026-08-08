package main

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "sendit",
	Short: "Realistic web traffic generator",
	Long: `sendit simulates realistic user web traffic across HTTP, headless
browser, DNS, and WebSocket protocols.

Targets are defined in a YAML config file under 'targets' (inline) and/or
loaded from a plain-text file via 'targets_file'. Both can be used together.

Use 'sendit probe <target>' to test a single endpoint interactively without
a config file — works like ping for HTTP, DNS, and WebSocket targets.

Use 'sendit pinch <host:port>' to check TCP/UDP port connectivity without
a config file.

Use 'sendit generate' to generate a ready-to-use config.yaml from a
targets file, a seed URL with in-domain crawling, or your local browser
history or bookmarks — no manual config editing required.

Use 'sendit export --pcap <results.jsonl>' to convert a results file to
PCAP format for analysis in Wireshark or similar tools.

Use 'sendit validate' to check a config before running.`,
}

func init() {
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(stopCmd())
	rootCmd.AddCommand(reloadCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(validateCmd())
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(probeCmd())
	rootCmd.AddCommand(pinchCmd())
	rootCmd.AddCommand(exportCmd())
	rootCmd.AddCommand(generateCmd())
}
