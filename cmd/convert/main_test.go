package main_test

import (
	"bytes"
	"testing"

	composek8s "github.com/slaskis/compose-k8s"
	main "github.com/slaskis/compose-k8s/cmd/convert"
	"github.com/slaskis/compose-k8s/internal/test"
)

func TestConvert(t *testing.T) {
	tests := []struct {
		Name string
		main.Options
	}{
		{Name: "secrets/k8s.yaml", Options: main.Options{
			Files:      []string{"testdata/secrets/compose.yaml"},
			Profiles:   []string{"*"},
			WorkingDir: "testdata/secrets/",
		}},
		{Name: "simple/k8s.yaml", Options: main.Options{
			Files:      []string{"testdata/simple/compose.yaml"},
			Profiles:   []string{"*"},
			WorkingDir: "testdata/simple/",
		}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			project, err := main.NewProject(tt.Options)
			if err != nil {
				t.Fatal(err)
			}
			resources, err := composek8s.Convert(project)
			if err != nil {
				t.Fatal(err)
			}
			buf := bytes.Buffer{}
			err = resources.Write(&buf)
			if err != nil {
				t.Fatal(err)
			}
			test.Snapshot(t, buf.Bytes())
		})
	}
}
