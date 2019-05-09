// Fake version of mokutil that only returns the possible states of secure
// boot when mokutil is called with --sb-state depending the whether the
// system is EFI with secure boot, EFI w/o secure boot or a legacy (ie BIOS)
// system.
// It doesn't call the real mokutil at all.
package main

import (
	"fmt"
	"os"
)

func main() {
	switch sb := os.Getenv("TEST_MOKUTIL_SECUREBOOT"); sb {
	case "efi-sb":
		fmt.Println("SecureBoot enabled")
	case "efi-nosb":
		fmt.Println("SecureBoot disabled")
	case "legacy":
		fmt.Fprint(os.Stderr, "EFI variables are not supported on this system")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "Unknown value: %s", sb)
		os.Exit(255)
	}
	os.Exit(0)
}
