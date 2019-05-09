package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const creationCmd = "zfs get -H creation "

func main() {
	cmdLine := strings.Join(os.Args, " ")
	args := os.Args[1:]

	if strings.HasPrefix(cmdLine, creationCmd) && len(os.Args) == 5 {
		args[2] = "org.zsys:creation.test"
	}

	cmd := exec.Command("/sbin/zfs", args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			os.Exit(exiterr.ExitCode())
		}
		fmt.Println("Unexpected error when trying to execute zfs", err)
		os.Exit(2)
	}
	os.Exit(0)
}
