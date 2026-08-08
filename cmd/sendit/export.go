package main

import (
	"fmt"

	"github.com/lewta/sendit/internal/pcap"
	"github.com/spf13/cobra"
)

// --- export ---

func exportCmd() *cobra.Command {
	var (
		pcapIn  string
		pcapOut string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export a results file to an alternative format",
		Long: `Convert a sendit result file to another format.

Currently supports converting a JSONL results file (written by the output
writer) to a synthetic PCAP file for analysis in Wireshark or tshark.

The PCAP uses LINKTYPE_USER0 (147) — no IP/TCP framing. Each packet payload
is a text record containing the URL, type, status code, latency, bytes, and
any error from the original request. No root or CAP_NET_RAW privilege is
required.

Examples:
  sendit export --pcap results.jsonl
  sendit export --pcap results.jsonl --output capture.pcap`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if pcapIn == "" {
				return fmt.Errorf("--pcap <results.jsonl> is required")
			}
			return pcap.Export(pcapIn, pcapOut)
		},
	}

	cmd.Flags().StringVar(&pcapIn, "pcap", "", "JSONL results file to convert to PCAP")
	cmd.Flags().StringVar(&pcapOut, "output", "", "Output PCAP file path (default: input file with .pcap extension)")

	return cmd
}
