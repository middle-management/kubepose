package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/slaskis/kubepose"
	"github.com/slaskis/kubepose/internal/project"
)

type Convert struct {
	Files    []string `name:"file" short:"f" desc:"Compose configuration files"` // []string{"compose.yaml"}
	Profiles []string `name:"profile" desc:"Specify a profile to enable"`        // []string{"*"}
	LogLevel string   `name:"log-level" short:"l" desc:"Log level" default:"info"`
}

func (cmd *Convert) Run() error {
	if level, err := logrus.ParseLevel(cmd.LogLevel); err != nil {
		logrus.SetLevel(level)
	}

	project, err := project.New(context.Background(), project.Options{
		Files:    cmd.Files,
		Profiles: cmd.Profiles,
	})
	if err != nil {
		return fmt.Errorf("unable to load files: %w", err)
	}

	if len(project.DisabledServices) > 0 {
		logrus.WithFields(logrus.Fields{
			"profiles":  strings.Join(project.Profiles, ", "),
			"available": strings.Join(project.DisabledServices.GetProfiles(), ", "),
		}).Warn("Some services were disabled because profiles did not match")
	}

	transformer := kubepose.Transformer{
		Annotations: map[string]string{
			"kubepose.version": getVersion(),
		},
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "kubepose",
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
