package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/CultureLinux/skipack/internal/cli"
	"github.com/CultureLinux/skipack/internal/config"
)

// version is injected at release time with -ldflags "-X main.version=<tag>".
var version = "dev"

// buildVersion falls back to the module version recorded by `go install`.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

func main() {
	store, err := config.DefaultStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "skipack:", err)
		os.Exit(1)
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "skipack:", err)
		os.Exit(1)
	}
	os.Exit(cli.App{Version: buildVersion(), Store: store, Root: root, Out: os.Stdout, Err: os.Stderr, PruneCache: true}.Run(os.Args[1:]))
}
