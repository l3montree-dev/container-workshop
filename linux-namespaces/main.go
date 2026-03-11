package main

import (
	"fmt"
	"os"
)

func main() {
	real := os.Getuid()
	effective := os.Geteuid()

	fmt.Printf("real uid:      %d\n", real)
	fmt.Printf("effective uid: %d\n", effective)

	if real != effective {
		fmt.Println("\n→ effective uid differs — setuid bit is active")
		fmt.Println("  this process has elevated privileges the caller doesn't have")
	} else {
		fmt.Println("\n→ both UIDs match — running as yourself, no privilege escalation")
	}
}
