package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/policy"
	"github.com/spf13/cobra"
)

// configPolicyFlags is the shared, typed operator boundary for commands that
// load target configs. Never populate it from YAML or ambient environment.
type configPolicyFlags struct {
	mode                                 string
	envNames, readRoots, privateNetworks []string
	outputRoot                           string
	overwrite                            bool
}

func (f *configPolicyFlags) register(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&f.mode, "config-trust", "trusted", "Config trust mode: trusted (compatibility) or restricted (operator grants only)")
	flags.StringArrayVar(&f.envNames, "allow-env", nil, "Grant an exact environment variable name (repeatable; no wildcards)")
	flags.StringArrayVar(&f.readRoots, "allow-read-root", nil, "Grant an absolute existing read directory (repeatable)")
	flags.StringVar(&f.outputRoot, "allow-output-root", "", "Grant one absolute existing output directory")
	flags.BoolVar(&f.overwrite, "allow-overwrite", false, "Grant output overwrite permission; explicit false narrows trusted mode")
	flags.StringArrayVar(&f.privateNetworks, "allow-private-network", nil, "Grant a nonpublic or special-use network CIDR (repeatable; mapped IPv6 prefixes rejected)")
}

func (f *configPolicyFlags) build(cmd *cobra.Command) (policy.Access, error) {
	g := policy.Grants{EnvNames: f.envNames, ReadRoots: f.readRoots}
	if cmd.Flags().Changed("allow-output-root") {
		if f.outputRoot == "" {
			return policy.Access{}, errors.New("config policy: --allow-output-root must not be empty")
		}
		g.OutputRoot = &f.outputRoot
	}
	if cmd.Flags().Changed("allow-overwrite") {
		g.Overwrite = &f.overwrite
	}
	if cmd.Flags().Changed("allow-private-network") {
		g.PrivateNetworks = make([]netip.Prefix, 0, len(f.privateNetworks))
		for _, value := range f.privateNetworks {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return policy.Access{}, errors.New("config policy: --allow-private-network must be a CIDR")
			}
			g.PrivateNetworks = append(g.PrivateNetworks, prefix)
		}
	}
	return policy.New(policy.Mode(f.mode), g)
}

func reportConfigPolicy(cmd *cobra.Command, cfg *config.Config, summary bool) error {
	s := cfg.Access().Summary()
	if s.Mode == policy.ModeTrusted && (s.AllEnv || s.AllRead || s.AllOutput || s.AllAddresses) {
		cmd.PrintErrln("Warning: trusted config has broad capabilities; use --config-trust=restricted with operator grants for external configs.")
	}
	if !summary {
		return nil
	}
	requested, err := json.Marshal(cfg.CapabilityRequests())
	if err != nil {
		return err
	}
	granted, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Config policy: mode=%s requested=%s granted=%s\n", s.Mode, requested, granted)
	return err
}

// reloadConfig advances authority only after the entire replacement is
// accepted. Failed loads and engine rejections retain the previous ceiling.
func reloadConfig(path string, current policy.Access, accept func(*config.Config) error) (policy.Access, error) {
	cfg, err := config.LoadWithPolicy(path, current)
	if err != nil {
		return current, err
	}
	if err := accept(cfg); err != nil {
		return current, err
	}
	return cfg.Access(), nil
}
