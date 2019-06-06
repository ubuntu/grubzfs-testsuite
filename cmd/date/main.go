package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	cmdLine := strings.Join(os.Args, " ")

	// mock date +%s by returning a "current date" far in the future (2033-05-18T03:33:20+00:00  @2000000000)
	if cmdLine == "date +%s" {
		fmt.Println("2000000000")
		os.Exit(0)
	}

	cmd := exec.Command("/bin/date", os.Args[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// FIXME: replace with go 1.12: os.Exit(exiterr.ExitCode())
			_ = exiterr
			os.Exit(1)
		}
		fmt.Println("Unexpected error when trying to execute date", err)
		os.Exit(2)
	}
	os.Exit(0)
}
