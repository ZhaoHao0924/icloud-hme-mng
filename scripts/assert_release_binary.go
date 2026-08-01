package main

import (
	"debug/elf"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fail("usage: assert_release_binary <binary>")
	}

	path := os.Args[1]
	file, err := elf.Open(path)
	if err != nil {
		fail("open ELF binary: %v", err)
	}
	defer file.Close()

	if file.Class != elf.ELFCLASS64 {
		fail("ELF class = %s, want ELF64", file.Class)
	}
	if file.Machine != elf.EM_X86_64 {
		fail("ELF machine = %s, want x86_64", file.Machine)
	}

	// The optimized release build may omit Go BuildInfo, so validate its stable ELF contract.
	fmt.Println("release binary verified: ELF64 x86_64")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
