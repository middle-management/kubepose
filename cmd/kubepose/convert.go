package main

import (
	"fmt"
	"os"

	"github.com/slaskis/kubepose"
	"github.com/slaskis/kubepose/internal/project"
)

type Convert struct {
	Files    []string `name:"file" short:"f" default:"compose.yaml" desc:"Compose configuration files"` // []string{"compose.yaml"}
	Profiles []string `name:"profile" default:"*" desc:"Specify a profile to enable"`                   // []string{"*"}
}

func (cmd *Convert) Run() error {
	project, err := project.New(project.Options{
		Files:    cmd.Files,
		Profiles: cmd.Profiles,
	})
	if err != nil {
		return fmt.Errorf("unable to load files: %w", err)
	}

	resources, err := kubepose.Convert(project)
	if err != nil {
		return fmt.Errorf("unable to convert: %w", err)
	}

	err = resources.Write(os.Stdout)
	if err != nil {
		return fmt.Errorf("unable to write resources to file: %w", err)
	}

	return nil
}
