package kubepose_test

import (
	"bytes"
	"os/exec"

	"testing"

	"github.com/slaskis/kubepose"
	"github.com/slaskis/kubepose/internal/project"
	"github.com/slaskis/kubepose/internal/test"
)

type TestRunFlag int

const (
	TestRunKubectlDryRun TestRunFlag = 1 << iota
	TestRunComposeDryRun
	TestRunComposeUp
)

func TestConvert(t *testing.T) {
	tests := []struct {
		Name   string
		Env    map[string]string
		DryRun TestRunFlag
		project.Options
	}{
		{Name: "secrets/k8s.yaml", Options: project.Options{
			Files:    []string{"testdata/secrets/compose.yaml"},
			Profiles: []string{"*"},
		}, Env: map[string]string{
			"KUBEPOSE_ENV_SECRET": "abc",
		}, DryRun: TestRunKubectlDryRun | TestRunComposeUp},
		{Name: "secrets/k8s+external.yaml", Options: project.Options{
			Files:    []string{"testdata/secrets/compose.yaml", "testdata/secrets/compose.external.yaml"},
			Profiles: []string{"*"},
		}, Env: map[string]string{
			"KUBEPOSE_ENV_SECRET": "abc",
		}, DryRun: TestRunKubectlDryRun},
		{Name: "simple/k8s.yaml", Options: project.Options{
			Files:    []string{"testdata/simple/compose.yaml"},
			Profiles: []string{"*"},
		}, DryRun: TestRunKubectlDryRun | TestRunComposeDryRun},
		{Name: "volumes/k8s.yaml", Options: project.Options{
			Files:    []string{"testdata/volumes/compose.yaml"},
			Profiles: []string{"*"},
		}, DryRun: TestRunKubectlDryRun | TestRunComposeDryRun},
		{Name: "group/k8s.yaml", Options: project.Options{
			Files:    []string{"testdata/group/compose.yaml"},
			Profiles: []string{"*"},
		}, DryRun: TestRunKubectlDryRun | TestRunComposeDryRun},
		{Name: "configs/k8s.yaml", Options: project.Options{
			Files:    []string{"testdata/configs/compose.yaml"},
			Profiles: []string{"*"},
		}, Env: map[string]string{
			"CONFIG_VAR": "abc",
		}, DryRun: TestRunKubectlDryRun | TestRunComposeUp},
		{Name: "expose/k8s.yaml", Options: project.Options{
			Files:    []string{"testdata/expose/compose.yaml"},
			Profiles: []string{"*"},
		}, DryRun: TestRunKubectlDryRun | TestRunComposeDryRun},
		{Name: "interpolation/k8s.yaml", Options: project.Options{
			Files:    []string{"testdata/interpolation/compose.yaml"},
			Profiles: []string{"*"},
		}, Env: map[string]string{
			"VAR_NOT_INTERPOLATED_BY_COMPOSE": "abc",
			"VAR_INTERPOLATED_BY_COMPOSE":     "gcr.io/google-containers/",
		}, DryRun: TestRunKubectlDryRun | TestRunComposeDryRun},
		{Name: "collector/a/k8s.yaml", Options: project.Options{
			Files:    []string{"testdata/collector/compose.yaml"},
			Profiles: []string{"*"},
		}, Env: map[string]string{
			"CONFIG": "a",
		}, DryRun: TestRunKubectlDryRun | TestRunComposeDryRun},
		{Name: "collector/b/k8s.yaml", Options: project.Options{
			Files:    []string{"testdata/collector/compose.yaml"},
			Profiles: []string{"*"},
		}, Env: map[string]string{
			"CONFIG": "b",
		}, DryRun: TestRunKubectlDryRun | TestRunComposeDryRun},
		{Name: "user/k8s.yaml", Options: project.Options{
			Files:    []string{"testdata/user/compose.yaml"},
			Profiles: []string{"*"},
		}, DryRun: TestRunKubectlDryRun | TestRunComposeDryRun},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.Name, func(t *testing.T) {
			if tt.Env != nil {
				for tKey, tValue := range tt.Env {
					t.Setenv(tKey, tValue)
				}
			} else {
				// can not use t.Parallel() if setting environment variables
				t.Parallel()
			}

			project, err := project.New(tt.Options)
			if err != nil {
				t.Fatal(err)
			}
			transformer := kubepose.Transformer{
				Annotations: map[string]string{
					"testing": "abc",
				},
			}
			resources, err := transformer.Convert(project)
			if err != nil {
				t.Fatal(err)
			}
			buf := &bytes.Buffer{}
			err = resources.Write(buf)
			if err != nil {
				t.Fatal(err)
			}
			test.Snapshot(t, buf.Bytes())

			if tt.DryRun&TestRunKubectlDryRun != 0 {
				cmd := exec.Command("kubectl", "apply", "-f=-", "--dry-run=client")
				cmd.Stdin = buf
				stdout, err := cmd.CombinedOutput()
				if err != nil {
					t.Logf("kubectl output: %s", stdout)
					t.Fatal(err)
				}
			}

			if tt.DryRun&TestRunComposeDryRun != 0 {
				args := []string{"compose"}
				for _, file := range tt.Files {
					args = append(args, "-f", file)
				}
				args = append(args, "up", "--dry-run")
				cmd := exec.Command("docker", args...)
				stdout, err := cmd.CombinedOutput()
				if err != nil {
					t.Logf("docker compose output: %s", stdout)
					t.Fatal(err)
				}
			}

			if tt.DryRun&TestRunComposeUp != 0 {
				args := []string{"compose"}
				for _, file := range tt.Files {
					args = append(args, "-f", file)
				}
				args = append(args, "up")
				cmd := exec.Command("docker", args...)
				stdout, err := cmd.CombinedOutput()
				if err != nil {
					t.Logf("docker compose output: %s", stdout)
					t.Fatal(err)
				}
			}

		})
	}
}
