package main

import (
	"context"
	"fmt"
	"os"

	"github.com/compose-spec/compose-go/v2/cli"
	composek8s "github.com/slaskis/compose-k8s"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	files := []string{"compose.yaml"}
	profiles := []string{"*"}
	workingDir, _ := os.Getwd()

	projectOptions, err := cli.NewProjectOptions(
		files,
		cli.WithOsEnv,
		cli.WithWorkingDirectory(workingDir),
		cli.WithInterpolation(true),
		cli.WithProfiles(profiles),
	)
	if err != nil {
		return fmt.Errorf("unable to create compose options: %w", err)
	}

	project, err := cli.ProjectFromOptions(context.Background(), projectOptions)
	if err != nil {
		return fmt.Errorf("unable to load files: %w", err)
	}

	resources, err := composek8s.Convert(project)
	if err != nil {
		return fmt.Errorf("unable to convert: %w", err)
	}

	// Write all resources to file
	if err := resources.Write(os.Stdout); err != nil {
		return fmt.Errorf("error writing resources to file: %w", err)
	}

	return nil
}
