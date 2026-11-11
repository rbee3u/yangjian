package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/rbee3u/yangjian/internal/envelope"
	"github.com/spf13/cobra"
)

type app struct {
	out        io.Writer
	err        io.Writer
	configPath string
	logLevel   string
	json       bool
	text       bool
}

func newApp(out, err io.Writer) *app {
	return &app{out: out, err: err, json: true}
}

func (a *app) command() *cobra.Command {
	root := &cobra.Command{
		Use:           "yangjian",
		Short:         "Local-first, evidence-backed EVM protocol research",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if a.text {
				a.json = false
			}
			return a.setupLogger()
		},
	}
	root.PersistentFlags().StringVar(&a.configPath, "config", "", "config file path")
	root.PersistentFlags().StringVar(&a.logLevel, "log-level", "info", "log level: debug, info, warn, error")
	root.PersistentFlags().BoolVar(&a.json, "json", true, "emit JSON envelope")
	root.PersistentFlags().BoolVar(&a.text, "text", false, "emit human-readable output")

	root.AddCommand(a.versionCommand())
	root.AddCommand(a.configCommand())
	root.AddCommand(a.contractCommand())
	root.AddCommand(a.protocolCommand())
	root.AddCommand(a.traceCommand())
	root.AddCommand(a.fixturesCommand())
	return root
}

func (a *app) setupLogger() error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(a.logLevel)); err != nil {
		return fmt.Errorf("invalid log level %q: %w", a.logLevel, err)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(a.err, &slog.HandlerOptions{Level: level})))
	return nil
}

type exitError struct {
	code int
}

func (e *exitError) Error() string {
	return fmt.Sprintf("exit code %d", e.code)
}

func exitCode(err error, fallback int) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return fallback
}

func (a *app) success(command string, data any, meta envelope.Meta, start time.Time) error {
	meta.DurationMS = time.Since(start).Milliseconds()
	env := envelope.Success(command, data, meta)
	if err := envelope.Write(a.out, env); err != nil {
		return &exitError{code: 1}
	}
	if code := envelope.ExitCode(env); code != 0 {
		return &exitError{code: code}
	}
	return nil
}

func (a *app) fail(command string, code envelope.Code, err error, start time.Time, meta envelope.Meta) error {
	meta.DurationMS = time.Since(start).Milliseconds()
	if !a.json {
		fmt.Fprintf(a.err, "error (%s): %v\n", code, err)
		return &exitError{code: envelope.NewError(code, "", nil).ExitCode()}
	}
	env := envelope.Failure(command, envelope.NewError(code, err.Error(), nil), meta)
	if writeErr := envelope.Write(a.out, env); writeErr != nil {
		return &exitError{code: 1}
	}
	return &exitError{code: envelope.ExitCode(env)}
}

func Execute() int {
	a := newApp(os.Stdout, os.Stderr)
	root := a.command()
	err := root.Execute()
	if err == nil {
		return 0
	}
	if code := exitCode(err, 0); code != 0 {
		return code
	}
	env := envelope.Failure("", envelope.NewError(envelope.CodeUsage, err.Error(), nil), envelope.Meta{})
	_ = envelope.Write(a.out, env)
	return 2
}
