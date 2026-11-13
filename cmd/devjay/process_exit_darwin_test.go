package main

import (
	"bytes"
	"debug/gosym"
	"debug/macho"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

// Check the final linked target, not just the standard-library source: the app's
// dependency graph can override global cgo imports (notably x/sys's libc_exit).
// DEVJAY_EXIT_BINARY also allows checking a development or release app artifact.
func TestFailedExecExitBinding(t *testing.T) {
	path := os.Getenv("DEVJAY_EXIT_BINARY")
	if path == "" {
		var err error
		path, err = os.Executable()
		if err != nil {
			t.Fatal(err)
		}
	}
	f, err := macho.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Symtab == nil || f.Dysymtab == nil {
		t.Fatal("exit binding check requires Mach-O dynamic symbols")
	}
	text, pcln := f.Section("__text"), f.Section("__gopclntab")
	if text == nil || pcln == nil {
		t.Fatal("missing Go code or function metadata")
	}
	data, err := pcln.Data()
	if err != nil {
		t.Fatal(err)
	}
	// go test may strip local linker symbols; Go's runtime metadata survives.
	table, err := gosym.NewTable(nil, gosym.NewLineTable(data, text.Addr))
	if err != nil {
		t.Fatal(err)
	}
	var pc uint64
	for _, function := range table.Funcs {
		if strings.TrimSuffix(function.Name, ".abi0") == "syscall.libc_exit_trampoline" {
			pc = function.Entry
			break
		}
	}
	if pc == 0 {
		t.Fatal("failed-exec exit trampoline not found; review this check for the new Go toolchain")
	}
	if pc < text.Addr || pc+5 > text.Addr+text.Size {
		t.Fatal("exit trampoline is outside executable text")
	}
	var code [5]byte
	if _, err := text.ReadAt(code[:], int64(pc-text.Addr)); err != nil {
		t.Fatal(err)
	}
	var target uint64
	switch f.Cpu {
	case macho.CpuArm64:
		instruction := binary.LittleEndian.Uint32(code[:4])
		if instruction>>26 != 0x5 { // B imm26, relative to this instruction.
			t.Fatalf("unexpected exit trampoline instruction: %x", instruction)
		}
		target = uint64(int64(pc) + int64(int32(instruction<<6)>>4))
	case macho.CpuAmd64:
		if code[0] != 0xe9 { // JMP rel32, relative to the following instruction.
			t.Fatalf("unexpected exit trampoline instruction: %x", code)
		}
		target = uint64(int64(pc) + 5 + int64(int32(binary.LittleEndian.Uint32(code[1:]))))
	default:
		t.Fatalf("unsupported CPU: %v", f.Cpu)
	}
	// debug/macho's high-level Section omits the reserved fields that map each
	// linker stub to its imported symbol. Read those fields from the load command.
	for _, load := range f.Loads {
		raw := load.Raw()
		if len(raw) < 4 || macho.LoadCmd(f.ByteOrder.Uint32(raw)) != macho.LoadCmdSegment64 {
			continue
		}
		reader := bytes.NewReader(raw)
		var segment macho.Segment64
		if err := binary.Read(reader, f.ByteOrder, &segment); err != nil {
			t.Fatal(err)
		}
		for range segment.Nsect {
			var section macho.Section64
			if err := binary.Read(reader, f.ByteOrder, &section); err != nil {
				t.Fatal(err)
			}
			if section.Flags&0xff != 0x8 || target < section.Addr || target >= section.Addr+section.Size {
				continue // S_SYMBOL_STUBS
			}
			if section.Reserve2 == 0 || (target-section.Addr)%uint64(section.Reserve2) != 0 {
				t.Fatal("exit target is not aligned to a linker stub")
			}
			index := uint64(section.Reserve1) + (target-section.Addr)/uint64(section.Reserve2)
			if index >= uint64(len(f.Dysymtab.IndirectSyms)) || f.Dysymtab.IndirectSyms[index] >= uint32(len(f.Symtab.Syms)) {
				t.Fatal("invalid indirect symbol index")
			}
			name := f.Symtab.Syms[f.Dysymtab.IndirectSyms[index]].Name
			if name != "__exit" { // Mach-O adds one underscore to C's _exit.
				t.Fatalf("failed-exec calls %s, want __exit; build/test with scripts/go.sh", name)
			}
			return
		}
	}
	t.Fatalf("no imported symbol found for exit target %#x", target)
}
