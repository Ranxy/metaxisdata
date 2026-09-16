// Command mxd is a client for a metaxisdata server, built for agents: every
// command writes one JSON document to stdout and reports progress on stderr.
package main

import (
	"os"

	"github.com/Ranxy/metaxisdata/cli/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
