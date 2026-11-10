package main

import (
	"fmt"
	"time"

	"github.com/rbee3u/yangjian/internal/config"
	"github.com/rbee3u/yangjian/internal/envelope"
	"github.com/spf13/cobra"
)

func (a *app) configCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Yangjian configuration",
	}
	var force bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write an example configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			path := a.configPath
			if path == "" {
				path = config.DefaultPath
			}
			if err := config.WriteDefault(path, force); err != nil {
				return a.fail("config init", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			if !a.json {
				fmt.Fprintf(a.out, "wrote %s\n", path)
				return nil
			}
			return a.success("config init", map[string]string{"path": path}, envelope.Meta{}, start)
		},
	}
	initCmd.Flags().BoolVar(&force, "force", false, "overwrite an existing configuration")
	cmd.AddCommand(initCmd)
	return cmd
}
