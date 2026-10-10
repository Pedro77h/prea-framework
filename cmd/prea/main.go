package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/urfave/cli/v3"
)

func main() {
	cmd := &cli.Command{
		Name:  "prea",
		Usage: "Preá framework CLI",
		Commands: []*cli.Command{
			runCommand(),
			generateCommand(),
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runCommand() *cli.Command {
	return &cli.Command{
		Name:  "run",
		Usage: "Run the project server (go run ./cmd/server)",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "port",
				Aliases: []string{"p"},
				Value:   8000,
				Usage:   "application port",
				Sources: cli.EnvVars("PORT"),
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			server := exec.CommandContext(ctx, "go", "run", "./cmd/server")
			server.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", cmd.Int("port")))
			server.Stdin, server.Stdout, server.Stderr = os.Stdin, os.Stdout, os.Stderr
			return server.Run()
		},
	}
}
