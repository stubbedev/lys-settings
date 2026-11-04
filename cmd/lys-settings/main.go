// Command lys-settings is the lys desktop's settings app, for lys-shell and
// lys-wm: every shell change is a runtime override saved to runtime.yaml as it
// lands, every lys-wm change an edit of its config file.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/stubbedev/lys-shell/settingspage"

	"github.com/stubbedev/lys-settings/internal/settings"
)

var version = "dev"

const usage = `Configure the lys desktop's shell and compositor

Usage: lys-settings [OPTIONS]

Options:
      --page <PAGE>  Open on this settings page
      --list-pages   Print the pages as JSON and exit
  -h, --help         Print help
  -V, --version      Print version
`

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("lys-settings", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}
	page := flags.String("page", "", "")
	list := flags.Bool("list-pages", false, "")
	long := flags.Bool("version", false, "")
	short := flags.Bool("V", false, "")
	switch err := flags.Parse(args); {
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(stdout, usage)
		return exitOK
	case err != nil:
		fmt.Fprintf(stderr, "error: %v\n\n%s", err, usage)
		return exitUsage
	case flags.NArg() != 0:
		fmt.Fprintf(stderr, "error: unexpected argument %q\n\n%s", flags.Arg(0), usage)
		return exitUsage
	}
	switch {
	case *long || *short:
		fmt.Fprintln(stdout, "lys-settings", version)
		return exitOK
	case *list:
		return report(stderr, listPages(stdout))
	}
	return report(stderr, settings.Run(*page))
}

func listPages(stdout io.Writer) error {
	data, err := settingspage.Encode(settings.ListedPages())
	if err != nil {
		return err
	}
	_, err = stdout.Write(data)
	return err
}

func report(stderr io.Writer, err error) int {
	if err == nil {
		return exitOK
	}
	fmt.Fprintln(stderr, "lys-settings:", err)
	return exitError
}
