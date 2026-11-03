// Command drill proves restic backups actually restore.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "drill: not implemented yet")
	return 2
}
