// Command profiles loads and unloads env profiles in the current terminal.
package main

import (
	"os"

	"github.com/ryanparsa/profiles/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
