package main

import (
	"fmt"
	"os"

	"github.com/xian0310567/casebook/internal/adapter/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
