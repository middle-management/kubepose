package main

import (
	"fmt"
	"os"

	"github.com/slaskis/kubepose"
	"github.com/slaskis/kubepose/internal/project"
)

type Convert struct {
	Files    []string `name:"file" short:"f" desc:"Compose configuration files"` // []string{"compose.yaml"}
	Profiles []string `name:"profile" desc:"Specify a profile to enable"`        // []string{"*"}
}

func (cmd *Convert) Run() error {
	project, err := project.New(project.Options{
		Files:    cmd.Files,
		Profiles: cmd.Profiles,
	})
	if err != nil {
		return fmt.Errorf("unable to load files: %w", err)
	}

	transformer := kubepose.Transformer{
		Annotations: map[string]string{
			kubepose.KubeposeVersionAnnotationKey: getVersion(),
		},
	}

	resources, err := transformer.Convert(project)
	if err != nil {
		return fmt.Errorf("unable to convert: %w", err)
	}

	err = resources.Write(os.Stdout)
	if err != nil {
		return fmt.Errorf("unable to write resources to file: %w", err)
	}

	return nil
}
