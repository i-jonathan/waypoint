// Command waypoint is the Waypoint kanban tracker.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
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

		OpenURL: openURL,
	})
	stop()
	os.Exit(code)
}

// openURL asks the desktop to open url in the default browser.
func openURL(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap it; the browser outlives this call
	return nil
}
