package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const importCmd = "zpool import -f -a -o cachefile=none -N"

func main() {
	cmdLine := strings.Join(os.Args, " ")
	args := os.Args[1:]

	if strings.HasPrefix(cmdLine, importCmd) {
		dir, ok := os.LookupEnv("TEST_POOL_DIR")
		if !ok {
			dir = "."
		}
		args = append(args, "-d", dir)
	}

	cmd := exec.Command("/sbin/zpool", args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// FIXME: replace with go 1.12: os.Exit(exiterr.ExitCode())
			_ = exiterr
			os.Exit(1)
		}
		fmt.Println("Unexpected error when trying to execute zpool", err)
		os.Exit(2)
	}
	os.Exit(0)
}
