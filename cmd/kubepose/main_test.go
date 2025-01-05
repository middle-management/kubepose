package main_test

import (
	"bytes"
	"os/exec"
	"testing"

	"github.com/slaskis/kubepose"
	main "github.com/slaskis/kubepose/cmd/kubepose"
	"github.com/slaskis/kubepose/internal/test"
)

func TestConvert(t *testing.T) {
	tests := []struct {
		Name   string
		Env    map[string]string
		DryRun bool
		main.Options
	}{
		{Name: "secrets/k8s.yaml", Options: main.Options{
			Files:    []string{"testdata/secrets/compose.yaml"},
			Profiles: []string{"*"},
		}, Env: map[string]string{
			"ACE_IDENTITY": "abc",
		}, DryRun: true},
		{Name: "simple/k8s.yaml", Options: main.Options{
			Files:    []string{"testdata/simple/compose.yaml"},
			Profiles: []string{"*"},
		}, DryRun: true},
		{Name: "volumes/k8s.yaml", Options: main.Options{
			Files:    []string{"testdata/volumes/compose.yaml"},
			Profiles: []string{"*"},
		}, DryRun: true},
		{Name: "expose/k8s.yaml", Options: main.Options{
			Files:    []string{"testdata/expose/compose.yaml"},
			Profiles: []string{"*"},
		}, DryRun: true},
		{Name: "interpolation/k8s.yaml", Options: main.Options{
			Files:    []string{"testdata/interpolation/compose.yaml"},
			Profiles: []string{"*"},
		}, Env: map[string]string{
			"VAR_NOT_INTERPOLATED_BY_COMPOSE": "abc",
			"VAR_INTERPOLATED_BY_COMPOSE":     "def",
		}, DryRun: true},
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

			project, err := main.NewProject(tt.Options)
			if err != nil {
				t.Fatal(err)
			}
			resources, err := kubepose.Convert(project)
			if err != nil {
				t.Fatal(err)
			}
			buf := &bytes.Buffer{}
			err = resources.Write(buf)
			if err != nil {
				t.Fatal(err)
			}
			test.Snapshot(t, buf.Bytes())

			if tt.DryRun {
				cmd := exec.Command("kubectl", "apply", "-f=-", "--dry-run=client")
				cmd.Stdin = buf
				stdout, err := cmd.CombinedOutput()
				if err != nil {
					t.Logf("kubectl output: %s", stdout)
					t.Fatal(err)
				}
			}
		})
	}
}
