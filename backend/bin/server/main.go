package main

import (
	"os"

	"github.com/Ranxy/metaxisdata/backend/bin/server/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
