package main

import (
	"log"
	"os"

	"github.com/tdewolff/argp"
)

var logger = log.New(os.Stderr, "", 0)

func main() {
	cmd := argp.NewCmd(&Main{}, "kubepose")
	cmd.AddCmd(&Convert{}, "convert", "Convert compose spec to kubernetes resources")
	cmd.AddCmd(&Version{}, "version", "Command version")
	cmd.Error = logger
	cmd.Parse()
}

type Main struct{}

func (cmd *Main) Run() error {
	return argp.ShowUsage
}
