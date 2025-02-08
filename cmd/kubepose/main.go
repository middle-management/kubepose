package main

import (
	"log"
	"os"

	"github.com/alexflint/go-arg"
)

var logger = log.New(os.Stderr, "", 0)

func main() {
	var args Main
	p := arg.MustParse(&args)

	err := func() error {
		switch {
		case args.Convert != nil:
			return args.Convert.Run()
		case args.Version != nil:
			return args.Version.Run()
		default:
			p.WriteHelp(os.Stderr)
			return nil
		}
	}()
	if err != nil {
		logger.Println(err)
	}
}

type Main struct {
	Convert *Convert `arg:"subcommand:convert" help:"Convert compose spec to kubernetes resources"`
	Version *Version `arg:"subcommand:version" help:"Command version"`
}
