package version

import (
	"runtime"
	"runtime/debug"
	"strconv"
)

// These are intended to be populated at build time via -ldflags.
// They still have sensible fallbacks (debug.ReadBuildInfo) if unset.
var (
	BuildVersion = "dev"
	GitSHA       = ""
	BuildTime    = ""
)

type Info struct {
	Service     string `json:"service"`
	Version     string `json:"version"`
	GitSHA      string `json:"git_sha,omitempty"`
	BuildTime   string `json:"build_time,omitempty"`
	VCSModified *bool  `json:"vcs_modified,omitempty"`
	GoVersion   string `json:"go_version"`
	GOOS        string `json:"go_os"`
	GOARCH      string `json:"go_arch"`
}

func Get(service string) Info {
	gitSHA := GitSHA
	buildTime := BuildTime
	var modified *bool

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if gitSHA == "" {
					gitSHA = s.Value
				}
			case "vcs.time":
				if buildTime == "" {
					buildTime = s.Value
				}
			case "vcs.modified":
				if modified == nil {
					if b, err := strconv.ParseBool(s.Value); err == nil {
						modified = &b
					}
				}
			}
		}
	}

	return Info{
		Service:     service,
		Version:     BuildVersion,
		GitSHA:      gitSHA,
		BuildTime:   buildTime,
		VCSModified: modified,
		GoVersion:   runtime.Version(),
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
	}
}
