package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	for _, binding := range []string{"exit", "_exit", "unexpected"} {
		t.Run(binding, func(t *testing.T) {
			// Exercise JSON escaping as well as normal paths.
			root := filepath.Join(t.TempDir(), "Go root with \"quotes\"")
			out := filepath.Join(t.TempDir(), "overlay with spaces")
			if err := os.MkdirAll(filepath.Join(root, "src", "syscall"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(out, 0700); err != nil {
				t.Fatal(err)
			}
			body := "package syscall\n//go:cgo_import_dynamic libc_exit " + binding + " \"/usr/lib/libSystem.B.dylib\"\n// unrelated code stays intact\n"
			assembly := "TEXT ·libc_exit_trampoline(SB),NOSPLIT,$0-0\n\tJMP\tlibc_exit(SB)\n"
			for _, arch := range []string{"arm64", "amd64"} {
				if err := os.WriteFile(filepath.Join(root, "src", "syscall", "zsyscall_darwin_"+arch+".go"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "src", "syscall", "zsyscall_darwin_"+arch+".s"), []byte(assembly), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := generate(root, out)
			if binding == "unexpected" {
				if err == nil {
					t.Fatal("accepted unknown toolchain source")
				}
				if _, err := os.Stat(filepath.Join(out, "overlay.json")); !os.IsNotExist(err) {
					t.Fatal("published overlay after validation failure")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(out, "overlay.json"))
			if err != nil {
				t.Fatal(err)
			}
			var overlay struct{ Replace map[string]string }
			if err := json.Unmarshal(data, &overlay); err != nil {
				t.Fatal(err)
			}
			if len(overlay.Replace) != 4 {
				t.Fatalf("replacements: %v", overlay.Replace)
			}
			for source, patched := range overlay.Replace {
				originalBody := body
				want := strings.Replace(body, "libc_exit "+binding, "libc_devjay_child_exit _exit", 1)
				if filepath.Ext(source) == ".s" {
					originalBody = assembly
					want = strings.Replace(assembly, "JMP\tlibc_exit(SB)", "JMP\tlibc_devjay_child_exit(SB)", 1)
				}
				original, err := os.ReadFile(source)
				if err != nil || string(original) != originalBody {
					t.Fatalf("installed source changed: %s, %v", original, err)
				}
				got, err := os.ReadFile(patched)
				if err != nil || string(got) != want {
					t.Fatalf("unexpected patched source: %s, %v", got, err)
				}
			}
		})
	}
}
