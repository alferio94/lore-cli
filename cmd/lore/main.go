package main

import (
	"os"

	"github.com/alferio94/lore-cli/internal/cli"
	"github.com/alferio94/lore-cli/internal/tui"
	"github.com/alferio94/lore-cli/internal/version"
)

func main() {
	app := cli.New("", os.Stdout, os.Stderr, version.Current())
	app.TUIRunner = tui.Run
	os.Exit(app.Run(os.Args[1:]))
}
