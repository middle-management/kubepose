package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/middle-management/kubepose"
	"github.com/middle-management/kubepose/internal/project"
	"github.com/sirupsen/logrus"
)

type Convert struct {
	Files    []string `arg:"--file,-f,separate" help:"Compose configuration files"`
	Profiles []string `arg:"--profile,separate" help:"Specify a compose profile to enable"`
	LogLevel string   `arg:"--log-level,-l" help:"Log level" default:"info"`
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
