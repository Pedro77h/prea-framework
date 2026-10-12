package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/urfave/cli/v3"
)

func runCommand() *cli.Command {
	return &cli.Command{
		Name:  "run",
		Usage: "Build and run the project server (./cmd/server)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "profile",
				Usage: "config profile: loads config/application-<profile>.yaml",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			dir, err := os.MkdirTemp("", "prea-run")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)

			bin := filepath.Join(dir, "server")
			build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/server")
			build.Stdout, build.Stderr = os.Stdout, os.Stderr
			if err := build.Run(); err != nil {
				return fmt.Errorf("build: %w", err)
			}

			// The server is run directly (not via go run) so it gets the signals
			// and can shut down gracefully; the CLI waits for it.
			signals := make(chan os.Signal, 1)
			signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
			defer signal.Stop(signals)

			server := exec.Command(bin)
			server.Env = os.Environ()
			if cmd.IsSet("profile") {
				server.Env = append(server.Env, "PREA_PROFILE="+cmd.String("profile"))
			}
			server.Stdin, server.Stdout, server.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := server.Start(); err != nil {
				return err
			}
			go func() {
				for sig := range signals {
					// Ctrl+C already reached the server through the terminal's process group.
					if sig != os.Interrupt {
						server.Process.Signal(sig)
					}
				}
			}()
			return server.Wait()
		},
	}
}
