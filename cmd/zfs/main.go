package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const creationCmd = "zfs get -pH creation "
const listCurrentSystemDatasetCmd = "zfs mount"

func main() {
	cmdLine := strings.Join(os.Args, " ")
	args := os.Args[1:]

	if strings.HasPrefix(cmdLine, creationCmd) && len(os.Args) == 5 {
		args[2] = "org.zsys:creation.test"
	}

	cmd := exec.Command("/sbin/zfs", args...)
	cmd.Stderr = os.Stderr

	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't create stdout pipe: %v", err)
		os.Exit(2)
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Unexpected error when trying to start zfs: %v", err)
		os.Exit(2)
	}
	currentRootDataset := os.Getenv("TEST_MOCKZFS_CURRENT_ROOT_DATASET")
	if cmdLine == listCurrentSystemDatasetCmd && currentRootDataset != "" {
		s := bufio.NewScanner(outPipe)
		for s.Scan() {
			t := s.Text()
			if strings.HasPrefix(t, currentRootDataset+" ") {
				t = currentRootDataset + " /"
			}
			fmt.Println(t)
		}
		err = s.Err()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Can't FILTER zfs command: %v", err)
			os.Exit(2)
		}
	} else {
		_, err = io.Copy(os.Stdout, outPipe)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Can't COPY zfs command: %v", err)
			os.Exit(2)
		}
	}

	cmd.Stdin = os.Stdin
	if err := cmd.Wait(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// FIXME: replace with go 1.12: os.Exit(exiterr.ExitCode())
			_ = exiterr
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Unexpected error when trying to execute zfs: %v", err)
		os.Exit(2)
	}
	os.Exit(0)
}
