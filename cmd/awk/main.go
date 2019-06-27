// Fake version of awk to use alternative versions depending on the environment variable TEST_AWK_BIN.
package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	awk := "/usr/bin/awk"
	if mawk, ok := os.LookupEnv("TEST_AWK_BIN"); ok && mawk != "" {
		awk = mawk
	}

	cmd := exec.Command(awk, os.Args[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// FIXME: replace with go 1.12: os.Exit(exiterr.ExitCode())
			_ = exiterr
			os.Exit(1)
		}
		fmt.Printf("Unexpected error when trying to execute %q: %v", awk, err)
		os.Exit(2)
	}
	os.Exit(0)

}
