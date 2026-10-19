package main

import (
	"fmt"
	"os"

	"github.com/cyklodev/skipack/internal/cli"
	"github.com/cyklodev/skipack/internal/config"
)

var version = "dev"

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
	os.Exit(cli.App{Version: version, Store: store, Root: root, Out: os.Stdout, Err: os.Stderr}.Run(os.Args[1:]))
}
