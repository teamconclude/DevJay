// Generate a build-local correction for Go's Darwin failed-exec exit path.
// Go 1.27.4 calls libc exit here, running inherited atexit handlers after fork.
// AppKit/CoreSpotlight handlers can crash or deadlock before exec. _exit avoids
// those callbacks while preserving the original launch error sent to the parent.
// Use a unique dynamic-import name: x/sys/unix also declares the global libc_exit
// symbol, and its exit binding can otherwise override syscall's corrected one.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func generate(goroot, output string) error {
	replacements := make(map[string]string)
	for _, arch := range []string{"arm64", "amd64"} {
		name := "zsyscall_darwin_" + arch + ".go"
		source := filepath.Join(goroot, "src", "syscall", name)
		body, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		old := []byte("//go:cgo_import_dynamic libc_exit exit \"/usr/lib/libSystem.B.dylib\"")
		fixed := []byte("//go:cgo_import_dynamic libc_exit _exit \"/usr/lib/libSystem.B.dylib\"")
		isolated := []byte("//go:cgo_import_dynamic libc_devjay_child_exit _exit \"/usr/lib/libSystem.B.dylib\"")
		switch {
		case bytes.Count(body, old) == 1 && bytes.Count(body, fixed) == 0:
			body = bytes.Replace(body, old, isolated, 1)
		case bytes.Count(body, old) == 0 && bytes.Count(body, fixed) == 1:
			body = bytes.Replace(body, fixed, isolated, 1)
		default:
			return fmt.Errorf("unrecognized Darwin exit binding in %s; review the failed-exec workaround for this Go version", source)
		}
		patched := filepath.Join(output, name)
		if err := os.WriteFile(patched, body, 0600); err != nil {
			return err
		}
		replacements[source] = patched

		// Keep the Go trampoline name and ABI; only its imported target changes.
		asmName := "zsyscall_darwin_" + arch + ".s"
		asmSource := filepath.Join(goroot, "src", "syscall", asmName)
		assembly, err := os.ReadFile(asmSource)
		if err != nil {
			return err
		}
		jump := []byte("JMP\tlibc_exit(SB)")
		if bytes.Count(assembly, jump) != 1 {
			return fmt.Errorf("unrecognized Darwin exit trampoline in %s; review the failed-exec workaround for this Go version", asmSource)
		}
		assembly = bytes.Replace(assembly, jump, []byte("JMP\tlibc_devjay_child_exit(SB)"), 1)
		asmPatched := filepath.Join(output, asmName)
		if err := os.WriteFile(asmPatched, assembly, 0600); err != nil {
			return err
		}
		replacements[asmSource] = asmPatched
	}
	data, err := json.Marshal(struct{ Replace map[string]string }{replacements})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "overlay.json"), data, 0600)
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go-overlay GOROOT OUTPUT_DIRECTORY")
		os.Exit(2)
	}
	if err := generate(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "DevJay Go overlay:", err)
		os.Exit(1)
	}
}
