package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	cmdLine := strings.Join(os.Args, " ")

	// mock date +%s by returning a "current date" far in the future (2042-01-01)
	if cmdLine == "date +%s" {
		fmt.Println("2272143600")
		os.Exit(0)
	}

	cmd := exec.Command("/bin/date", os.Args[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			os.Exit(exiterr.ExitCode())
		}
		fmt.Println("Unexpected error when trying to execute date", err)
		os.Exit(2)
	}
	os.Exit(0)
}
