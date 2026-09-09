package main

import (
	"io"
	"os"

	"github.com/alferio94/lore-cli/internal/cli"
	"github.com/alferio94/lore-cli/internal/install"
	"github.com/alferio94/lore-cli/internal/releaseprofile"
	"github.com/alferio94/lore-cli/internal/tui"
	"github.com/alferio94/lore-cli/internal/version"
)

func main() {
	app := newApp(os.Stdout, os.Stderr)
	os.Exit(app.Run(os.Args[1:]))
}

func newApp(stdout, stderr io.Writer) *cli.App {
	app := cli.New("", stdout, stderr, version.Current())
	policy := install.NewRoutePolicyFromReleaseProfile(releaseprofile.Current())
	app.InstallWorkflow = install.NewCanonicalWorkflow(policy, install.TransactionInput{})
	app.TUIRunner = tui.Run
	return app
}
