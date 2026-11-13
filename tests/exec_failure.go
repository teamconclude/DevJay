//go:build ignore

// Native Go/libc regression harness; run with scripts/go.sh run.
package main

/*
#include <stdlib.h>
#include <unistd.h>

static pid_t original_pid;
static int observer_fd;
static void inherited_exit_handler(void) {
    if (getpid() != original_pid) {
        // This close-on-exec descriptor survives failed launches, including
        // failures before Go shuffles the child's standard descriptors.
        // Only async-signal-safe code runs here; no intentional crash is needed.
        const char marker = 'x';
        write(observer_fd, &marker, 1);
    }
}
static int register_exit_handler(int fd) {
    original_pid = getpid();
    observer_fd = fd;
    return atexit(inherited_exit_handler);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func probe(path, dir string, want syscall.Errno) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.Dir = dir
	err := cmd.Run()
	if want == 0 && err != nil || want != 0 && !errors.Is(err, want) {
		return fmt.Errorf("%s: got %v, want errno %v", path, err, want)
	}
	return nil
}

func check() error {
	// DevJay links x/sys/unix too. Its libc_exit dynamic import shares a global
	// linker symbol with syscall/runtime, even when unix.Exit is never called.
	if unix.Getpid() != os.Getpid() {
		return errors.New("unexpected process identity")
	}
	read, write, err := os.Pipe()
	if err != nil {
		return err
	}
	defer read.Close()
	defer write.Close()
	if C.register_exit_handler(C.int(write.Fd())) != 0 {
		return errors.New("could not register native exit handler")
	}
	root, err := os.MkdirTemp("", "devjay-exec-failure-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	for name, body := range map[string]string{
		"denied":      "#!/bin/sh\nexit 0\n",
		"invalid":     "not an executable\n",
		"interpreter": "#!" + filepath.Join(root, "missing-interpreter") + "\n",
	} {
		mode := os.FileMode(0700)
		if name == "denied" {
			mode = 0600
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), mode); err != nil {
			return err
		}
	}
	cases := []struct {
		path, dir string
		errno     syscall.Errno
	}{
		{filepath.Join(root, "missing"), "", syscall.ENOENT},
		{filepath.Join(root, "denied"), "", syscall.EACCES},
		{filepath.Join(root, "invalid"), "", syscall.ENOEXEC},
		{filepath.Join(root, "interpreter"), "", syscall.ENOENT},
		{"/usr/bin/true", filepath.Join(root, "missing-directory"), syscall.ENOENT},
		{"/usr/bin/true", "", 0},
	}
	// Concurrent launches reproduce the multithreaded app's process environment.
	var workers sync.WaitGroup
	results := make(chan error, len(cases)*4)
	for range 4 {
		workers.Go(func() {
			for _, test := range cases {
				results <- probe(test.path, test.dir, test.errno)
			}
		})
	}
	workers.Wait()
	close(results)
	write.Close()
	if err := read.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	callback, readErr := io.ReadAll(read)
	if readErr != nil || len(callback) != 0 {
		return fmt.Errorf("inherited exit handlers ran (%q), read error: %v", callback, readErr)
	}
	for err := range results {
		if err != nil {
			return err
		}
	}
	fmt.Println("passed 24 native exec checks: launch errors preserved, inherited exit handlers skipped")
	return nil
}

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
