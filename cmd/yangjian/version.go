package main

import (
	"fmt"
	"time"

	"github.com/rbee3u/yangjian/internal/envelope"
	"github.com/rbee3u/yangjian/internal/version"
	"github.com/spf13/cobra"
)

func (a *app) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			info := version.Current()
			if !a.json {
				fmt.Fprintln(a.out, info.String())
				return nil
			}
			return a.success("version", info, envelope.Meta{}, start)
		},
	}
}
