package main

import (
	"auto-code/internal/logging"
	"auto-code/internal/server"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"go.uber.org/zap"
)

// main initializes infrastructure logging and runs the HTTP server process.
func main() {
	if err := applyStartupArgs(os.Args[1:]); err != nil {
		logging.Init()
		defer logging.Sync()
		logging.Named("main").Fatal("invalid startup arguments", zap.Error(err))
	}

	logging.Init()
	defer logging.Sync()

	if err := server.Run(); err != nil {
		logging.Named("main").Fatal("server failed", zap.Error(err))
	}
}

// applyStartupArgs parses supported startup flags and mirrors values to env vars.
func applyStartupArgs(args []string) error {
	fs := flag.NewFlagSet("auto-code", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var zhiPuAuthToken string
	fs.StringVar(&zhiPuAuthToken, "zhipu-auth-token", "", "set ZHI_PU_AUTH_TOKEN for runtime profile env resolution")
	fs.StringVar(&zhiPuAuthToken, "ZHI_PU_AUTH_TOKEN", "", "set ZHI_PU_AUTH_TOKEN for runtime profile env resolution")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse startup args: %w", err)
	}

	token := strings.TrimSpace(zhiPuAuthToken)
	if token == "" {
		return nil
	}
	if err := os.Setenv("ZHI_PU_AUTH_TOKEN", token); err != nil {
		return fmt.Errorf("set ZHI_PU_AUTH_TOKEN: %w", err)
	}
	return nil
}
