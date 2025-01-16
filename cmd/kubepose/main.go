package main

import (
	"log"
	"os"

	"github.com/tdewolff/argp"
)

func main() {
	cmd := argp.NewCmd(&Main{}, "kubepose")
	cmd.AddCmd(&Convert{}, "convert", "Convert compose spec to kubernetes resources")
	cmd.AddCmd(&Version{version: version}, "version", "Command version")
	cmd.Error = log.New(os.Stderr, "", 0)
	cmd.Parse()
}

type Main struct{}

func (cmd *Main) Run() error {
	return argp.ShowUsage
}
