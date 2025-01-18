package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"
)

type Version struct {
}

// this is set using `-ldflags "-X main.version=1.2.3"`
var version string

const defaultVersion = "devel"

func (cmd *Version) Run() error {
	_, err := fmt.Fprintln(os.Stdout, getVersion())
	return err
}

func getVersion() string {
	if version != "" {
		return version
	}

	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return defaultVersion
	}

	var vcs struct {
		revision string
		time     time.Time
		modified bool
	}
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			vcs.revision = setting.Value
		case "vcs.time":
			vcs.time, _ = time.Parse(time.RFC3339, setting.Value)
		case "vcs.modified":
			vcs.modified = (setting.Value == "true")
		}
	}

	if s := buildInfo.Main.Version; s != "" && s != "(devel)" {
		return s
	}

	var b strings.Builder
	b.WriteString(defaultVersion)
	b.WriteString(" (")
	if vcs.revision == "" || len(vcs.revision) < 12 {
		b.WriteString("unknown revision")
	} else {
		b.WriteString(vcs.revision[:12])
	}
	if !vcs.time.IsZero() {
		b.WriteString(", " + vcs.time.Format(time.DateTime))
	}
	if vcs.modified {
		b.WriteString(", dirty")
	}
	b.WriteString(")")
	return b.String()
}
