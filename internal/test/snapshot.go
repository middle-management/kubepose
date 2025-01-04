package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/tidwall/sjson"
)

// Snapshot either writes or compares with a file in ./testdata
func Snapshot(t *testing.T, data any, except ...string) {
	t.Helper()

	var actual []byte
	var isJSON bool
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
		isJSON = true
	}

	golden := filepath.Join(".", "testdata", t.Name())
	var opt cmp.Option
	if isJSON {
		for _, e := range except {
			a, err := sjson.SetBytes(actual, e, "<excluded>")
			if err != nil {
				t.Fatal(err)
			}
			actual = a
		}
		golden += ".json"

		opt = cmp.FilterValues(func(x, y []byte) bool {
			return json.Valid(x) && json.Valid(y)
		}, cmp.Transformer("ParseJSON", func(in []byte) (out interface{}) {
			if err := json.Unmarshal(in, &out); err != nil {
				panic(err) // should never occur given previous filter to ensure valid JSON
			}
			return out
		}))
	}

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

	if !cmp.Equal(expected, actual, opt) {
		t.Log(cmp.Diff(expected, actual, opt))
		t.Fatal("snapshot does not match")
	}
}
