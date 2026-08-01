package main

import (
	"debug/buildinfo"
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

	info, err := buildinfo.ReadFile(path)
	if err != nil {
		fail("read Go build info: %v", err)
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	for key, want := range map[string]string{"GOARCH": "amd64", "GOOS": "linux"} {
		if got := settings[key]; got != want {
			fail("build setting %s = %q, want %q", key, got, want)
		}
	}

	fmt.Println("release binary verified: ELF64 x86_64, GOOS=linux, GOARCH=amd64")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
