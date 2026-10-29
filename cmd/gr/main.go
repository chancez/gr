// Command gr is local code review for git.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/chancez/gr/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Execute(ctx)
	stop()
	os.Exit(code)
}
