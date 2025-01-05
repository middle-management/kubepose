package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Snapshot either writes or compares with a file in ./testdata
func Snapshot(t *testing.T, data any, opt ...cmp.Option) {
	t.Helper()

	var actual []byte
	switch d := data.(type) {
	case string:
		actual = []byte(d)
	case []byte:
		actual = d
	default:
		a, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		actual = a
	}

	golden := filepath.Join(".", "testdata", t.Name())
	if _, err := os.Stat(golden); os.IsNotExist(err) {
		// generate new snapshot
		_ = os.MkdirAll(filepath.Dir(golden), 0o750)
		err := os.WriteFile(golden, actual, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		return
	}

	expected, err := os.ReadFile(filepath.Clean(golden))
	if err != nil {
		t.Fatal(err)
	}

	if !cmp.Equal(expected, actual, opt...) {
		t.Log(cmp.Diff(expected, actual, opt...))
		t.Fatal("snapshot does not match")
	}
}
