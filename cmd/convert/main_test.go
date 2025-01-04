package main_test

import (
	"bytes"
	"path/filepath"
	"testing"

	composek8s "github.com/slaskis/compose-k8s"
	main "github.com/slaskis/compose-k8s/cmd/convert"
	"github.com/slaskis/compose-k8s/internal/test"
)

func TestConvert(t *testing.T) {
	tests := []struct {
		ComposeFiles []string
	}{
		{ComposeFiles: []string{"testdata/secrets/compose.yaml"}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.ComposeFiles[0], func(t *testing.T) {
			t.Parallel()
			project, err := main.NewProject(main.Options{
				Files:      tt.ComposeFiles,
				Profiles:   []string{"*"},
				WorkingDir: filepath.Dir(tt.ComposeFiles[0]),
			})
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
