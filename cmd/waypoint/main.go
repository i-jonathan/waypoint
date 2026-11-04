// Command waypoint is the Waypoint kanban tracker.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"waypoint/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "waypoint: cannot find the working directory: %v\n", err)
		cwd = ""
	}
	code := cli.Run(ctx, os.Args[1:], cli.Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
		Cwd:    cwd,
		Now:    time.Now,
	})
	stop()
	os.Exit(code)
}
