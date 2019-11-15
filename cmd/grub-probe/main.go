package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		// Avoid a panic if no args have been provided and return the same code than the real grub-probe
		fmt.Fprintln(os.Stderr, `No path or device is specified.
Usage: grub-probe [OPTION...] [OPTION]... [PATH|DEVICE]
Try 'grub-probe --help' or 'grub-probe --usage' for more information.`)
		os.Exit(64)
	}

	switch os.Args[1] {
	case "--target=device":
		cmd := exec.Command("/usr/sbin/grub-probe", os.Args[1:]...)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		cmd.Stdin = os.Stdin
		if err := cmd.Run(); err != nil {
			if exiterr, ok := err.(*exec.ExitError); ok {
				// FIXME: replace with go 1.12: os.Exit(exiterr.ExitCode())
				_ = exiterr
				os.Exit(1)
			}
			fmt.Println("Unexpected error when trying to execute grube-prove", err)
			os.Exit(2)
		}
		os.Exit(0)
	case "--device":
		if !strings.HasPrefix(os.Args[3], "--target") {
			break
		}
		v := map[string]string{
			"abstraction":        "modfor_" + os.Args[2],
			"compatibility_hint": "hd0,gpt2",
			"fs":                 "ext2",
			"fs_uuid":            "UUID-" + os.Args[2],
			"partmap":            "gpt",
			"hints_string":       "--hint-bios=hd0,gpt2 --hint-efi=hd0,gpt2",
		}
		fmt.Println(v[strings.Split(os.Args[3], "=")[1]])
		os.Exit(0)
	case "--target=abstraction":
		os.Exit(0)
	case "--target=fs":
		fmt.Println("ext2")
		os.Exit(0)
	}

	fmt.Fprintln(os.Stderr, "grub-probe called with unexpected arguments:", strings.Join(os.Args, " "))
	os.Exit(2)
}
