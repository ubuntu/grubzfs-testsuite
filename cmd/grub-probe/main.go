package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {

	switch os.Args[1] {
	case "--target=device":
		fmt.Println("UUID-" + os.Args[2])
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
