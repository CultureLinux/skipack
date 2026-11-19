package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/CultureLinux/skipack/internal/config"
	gh "github.com/CultureLinux/skipack/internal/github"
	"github.com/CultureLinux/skipack/internal/install"
	"github.com/CultureLinux/skipack/internal/selfupdate"
)

type SelfUpdater interface {
	Update() error
}

type App struct {
	Version     string
	Store       config.Store
	Root        string
	Out, Err    io.Writer
	GitHub      *gh.Client
	Fetcher     install.Fetcher
	SelfUpdater SelfUpdater
	// PruneCache bounds the on-disk GitHub cache after network commands (set by main).
	PruneCache bool
}

func (a App) fail(format string, args ...any) int {
	fmt.Fprintf(a.Err, "skipack: "+format+"\n", args...)
	return 1
}

func (a App) usage() int {
	a.help(a.Err)
	return 2
}

func (a App) client() *gh.Client {
	if a.GitHub != nil {
		return a.GitHub
	}
	return gh.NewClient()
}

func (a App) load() (config.Config, error) {
	source, err := a.Store.Get()
	if err != nil {
		return config.Config{}, err
	}
	var b []byte
	if strings.HasPrefix(source, "https://") {
		b, err = a.client().ReadConfigURL(source)
	} else {
		b, err = os.ReadFile(source)
	}
	if err != nil {
		return config.Config{}, fmt.Errorf("load configuration: %w", err)
	}
	return config.Parse(b)
}

func (a App) manager() install.Manager {
	f := a.Fetcher
	if f == nil {
		f = a.client()
	}
	return install.Manager{Root: a.Root, Fetcher: f}
}

func (a App) selfUpdater() SelfUpdater {
	if a.SelfUpdater != nil {
		return a.SelfUpdater
	}
	return selfupdate.New()
}

func (a App) pruneCache() {
	if a.PruneCache {
		_ = gh.NewClient().PruneCache(30*24*time.Hour, 500<<20)
	}
}

// commands maps each command name to its handler. Handlers receive the full
// argument list, command name included, and return the exit code.
var commands = map[string]func(App, []string) int{
	"self-update": App.selfUpdate,
	"version":     App.version,
	"--version":   App.version,
	"help":        App.helpCommand,
	"-h":          App.helpCommand,
	"--help":      App.helpCommand,
	"config":      App.configCommand,
	"agent":       App.agentCommand,
	"list":        App.list,
	"audit":       App.audit,
	"add":         App.add,
	"delete":      App.delete,
	"update":      App.update,
}

func (a App) Run(args []string) int {
	if a.Out == nil {
		a.Out = os.Stdout
	}
	if a.Err == nil {
		a.Err = os.Stderr
	}
	if len(args) == 0 {
		a.help(a.Out)
		return 0
	}
	handler, ok := commands[args[0]]
	if !ok {
		return a.usage()
	}
	switch args[0] {
	case "add", "update", "list", "audit":
		defer a.pruneCache()
	}
	return handler(a, args)
}
