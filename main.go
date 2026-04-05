package main

import "github.com/fractalops/ssmx/cmd"

// version and buildTime are set at build time via ldflags:
//
//	-X main.version=v1.2.3
//	-X main.buildTime=2025-01-01_12:00:00
var (
	version   = "dev"
	buildTime = ""
)

func main() {
	cmd.Execute(version, buildTime)
}
