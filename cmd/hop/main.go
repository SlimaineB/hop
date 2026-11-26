package main

import (
	"os"

	"github.com/SlimaineB/hop/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
