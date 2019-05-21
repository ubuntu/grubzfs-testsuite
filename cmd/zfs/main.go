package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const creationCmd = "zfs get -H creation "
const listCurrentSystemDatasetCmd = "zfs list -H -oname,mounted,mountpoint -t filesystem"

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
		fmt.Println("Can't create stdout pipe", err)
		os.Exit(2)
	}

	go func() {
		var err error

		currentRootDataset := os.Getenv("TEST_MOCKZFS_CURRENT_ROOT_DATASET")
		if cmdLine == listCurrentSystemDatasetCmd && currentRootDataset != "" {
			s := bufio.NewScanner(outPipe)
			for s.Scan() {
				t := s.Text()
				if strings.HasPrefix(t, currentRootDataset+"\t") {
					t = strings.ReplaceAll(t, "\tno\t", "\tyes\t")
				}
				fmt.Println(t)
			}
			err = s.Err()
		} else {
			_, err = io.Copy(os.Stdout, outPipe)
		}

		if err != nil {
			fmt.Println("Can't output zfs command", err)
			os.Exit(2)
		}
	}()

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
