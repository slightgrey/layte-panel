package main

import (
	"flag"
	"path/filepath"

	"layte-panel/internal/config"
)

// configFlags are the --config/--dev flags every subcommand shares.
type configFlags struct {
	path *string
	dev  *bool
}

func addConfigFlags(fs *flag.FlagSet) configFlags {
	return configFlags{
		path: fs.String("config", "", "path to config.toml (default "+config.DefaultPath+", or .dev/config.toml with --dev)"),
		dev:  fs.Bool("dev", false, "development mode: local ./.dev paths, no Secure cookie, CORS for the Vite dev server, fake agent runner"),
	}
}

func (f configFlags) load() (config.Config, error) {
	p := *f.path
	if p == "" {
		p = config.DefaultPath
		if *f.dev {
			p = filepath.Join(".dev", "config.toml")
		}
	}
	return config.Load(p, *f.dev)
}
