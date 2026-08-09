package main

import (
	"flag"
	"fmt"

	"github.com/omriharel/deej/pkg/deej"
)

var (
	gitCommit  string
	versionTag string
	buildType  string

	verbose bool
	debug   bool
)

func init() {
	flag.BoolVar(&verbose, "verbose", false, "show verbose logs (useful for debugging serial)")
	flag.BoolVar(&verbose, "v", false, "shorthand for --verbose")
	flag.BoolVar(&debug, "debug", false, "show debug logs (useful for debugging audio session detection), even in release builds")
	flag.BoolVar(&debug, "d", false, "shorthand for --debug")
	flag.Parse()
}

func main() {

	// first we need a logger
	logger, err := deej.NewLogger(buildType, debug)
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}

	named := logger.Named("main")
	named.Debug("Created logger")

	named.Infow("Version info",
		"gitCommit", gitCommit,
		"versionTag", versionTag,
		"buildType", buildType)

	// provide a fair warning if the user's running in verbose mode
	if verbose {
		named.Debug("Verbose flag provided, all log messages will be shown")
	}

	if debug {
		named.Debug("Debug flag provided, debug logs will be shown even in a release build")
	}

	// create the deej instance
	d, err := deej.NewDeej(logger, verbose)
	if err != nil {
		named.Fatalw("Failed to create deej object", "error", err)
	}

	// if injected by build process, set version info to show up in the tray
	if buildType != "" && (versionTag != "" || gitCommit != "") {
		identifier := gitCommit
		if versionTag != "" {
			identifier = versionTag
		}

		versionString := fmt.Sprintf("Version %s-%s", buildType, identifier)
		d.SetVersion(versionString)
	}

	// onwards, to glory
	if err = d.Initialize(); err != nil {
		named.Fatalw("Failed to initialize deej", "error", err)
	}
}
