package project

import (
	"context"
	"fmt"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
)

type Options struct {
	Files    []string
	Profiles []string
}

func New(opts Options) (*types.Project, error) {
	projectOptions, err := cli.NewProjectOptions(
		opts.Files,
		cli.WithOsEnv,
		cli.WithConfigFileEnv,
		cli.WithDefaultConfigPath,
		cli.WithInterpolation(true),
		cli.WithProfiles(opts.Profiles),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create compose options: %w", err)
	}

	project, err := cli.ProjectFromOptions(context.Background(), projectOptions)
	if err != nil {
		return nil, fmt.Errorf("unable to load files: %w", err)
	}

	return project, nil
}
