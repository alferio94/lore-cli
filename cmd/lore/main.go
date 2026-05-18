package main

import (
	"os"

	"github.com/alferio94/lore-cli/internal/cli"
	"github.com/alferio94/lore-cli/internal/tui"
)

func main() {
	app := cli.New("", os.Stdout, os.Stderr)
	app.TUIRunner = tui.Run
	os.Exit(app.Run(os.Args[1:]))
}
