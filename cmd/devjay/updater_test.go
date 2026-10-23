package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUpdateVersionOrdering(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"0.8.1", "0.8.0", true}, {"0.10.0", "0.9.9", true}, {"1.0.0", "0.99.9", true}, {"0.8.0", "0.8.0", false}, {"0.7.9", "0.8.0", false}, {"01.8.0", "0.8.0", false}, {"0.8.1-beta", "0.8.0", false}, {"0.8.1", "invalid", false}} {
		if got := newerRelease(c.a, c.b); got != c.want {
			t.Fatalf("%+v: %v", c, got)
		}
	}
}
func TestUpdateMetadataAndCorruptDownload(t *testing.T) {
	version := "0.8.1"
	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte("expected")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "version.txt"):
			fmt.Fprintln(w, version)
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			fmt.Fprintf(w, "%s  DevJay-%s-macos-universal.zip\n", checksum, version)
		default:
			fmt.Fprint(w, "corrupt")
		}
	}))
	defer server.Close()
	parent := t.TempDir()
	bundle := filepath.Join(parent, "DevJay.app")
	os.Mkdir(bundle, 0700)
	u := appUpdater{client: server.Client(), version: "0.8.0", bundle: bundle}
	r, err := u.latest(context.Background(), server.URL)
	if err != nil || r == nil {
		t.Fatal(r, err)
	}
	if _, err = u.download(context.Background(), r, func(int) {}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(parent)
	if len(files) != 1 {
		t.Fatal("failed download left staging behind")
	}
	version = "0.8.0"
	if r, err = u.latest(context.Background(), server.URL); err != nil || r != nil {
		t.Fatal(r, err)
	}
	version = "../bad"
	if _, err = u.latest(context.Background(), server.URL); err == nil {
		t.Fatal("invalid version accepted")
	}
	version = "0.8.1"
	checksum = "bad"
	if _, err = u.latest(context.Background(), server.URL); err == nil {
		t.Fatal("invalid checksum accepted")
	}
}
func TestUpdateArchivePaths(t *testing.T) {
	for _, name := range []string{"../escape", "Other.app/x", "DevJay.app/../../escape", "/DevJay.app/x", "DevJay.app/Contents/Info.plist"} {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		w, _ := z.Create(name)
		w.Write([]byte("x"))
		z.Close()
		file := filepath.Join(t.TempDir(), "a.zip")
		os.WriteFile(file, buf.Bytes(), 0600)
		err := validateUpdateArchive(file)
		if (err == nil) != (name == "DevJay.app/Contents/Info.plist") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
func signedUpdateFixture(t *testing.T, app string) {
	t.Helper()
	os.MkdirAll(filepath.Join(app, "Contents/MacOS"), 0700)
	exe, err := os.ReadFile("/usr/bin/true")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(app, "Contents/MacOS/DevJay"), exe, 0700)
	plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>io.teamconclude.devjay</string><key>CFBundleExecutable</key><string>DevJay</string><key>CFBundleShortVersionString</key><string>0.8.1</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>`
	os.WriteFile(filepath.Join(app, "Contents/Info.plist"), []byte(plist), 0600)
	if out, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", app).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
}
func TestUpdateHelperWaitsAndPreservesOldAppOnFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			root := t.TempDir()
			bundle := filepath.Join(root, "DevJay.app")
			os.Mkdir(bundle, 0700)
			os.WriteFile(filepath.Join(bundle, "original"), []byte("old"), 0600)
			stage := filepath.Join(root, "stage")
			newBundle := filepath.Join(stage, "unpacked/DevJay.app")
			signedUpdateFixture(t, newBundle)
			if err := verifyUpdateBundle(context.Background(), newBundle, "0.8.1"); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(root, "bin")
			os.Mkdir(bin, 0700)
			os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\nexit 0\n"), 0700)
			if fail {
				os.WriteFile(filepath.Join(bin, "mv"), []byte("#!/bin/bash\nif [[ \"$1\" == */unpacked/DevJay.app ]]; then exit 1; fi\nexec /bin/mv \"$@\"\n"), 0700)
			}
			sleeper := exec.Command("/bin/cat")
			input, err := sleeper.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			if err := sleeper.Start(); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("/bin/bash", "-c", updateScript, "update", strconv.Itoa(sleeper.Process.Pid), bundle, stage, "0.8.1")
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
			if _, err := os.Stat(filepath.Join(bundle, "original")); err != nil {
				t.Fatal("replaced live bundle")
			}
			input.Close()
			sleeper.Wait()
			err = cmd.Wait()
			if fail {
				if err == nil {
					t.Fatal("swap unexpectedly succeeded")
				}
				if _, err := os.Stat(filepath.Join(bundle, "original")); err != nil {
					t.Fatal("old app lost")
				}
			} else {
				if err != nil {
					t.Fatalf("%v: %s", err, output.String())
				}
				if err := verifyUpdateBundle(context.Background(), bundle, "0.8.1"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestUpdateDownloadsVerifiedBundleWithoutReplacingRunningApp(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source/DevJay.app")
	signedUpdateFixture(t, source)
	archive := filepath.Join(root, "release.zip")
	if err := updateCommand(context.Background(), "/usr/bin/ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", source, archive); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer server.Close()
	bundle := filepath.Join(root, "DevJay.app")
	os.Mkdir(bundle, 0700)
	os.WriteFile(filepath.Join(bundle, "original"), []byte("old"), 0600)
	u := appUpdater{client: server.Client(), bundle: bundle, version: "0.8.0"}
	r := &updateRelease{version: "0.8.1", url: server.URL, checksum: fmt.Sprintf("%x", sha256.Sum256(body))}
	progress := -1
	dir, err := u.download(context.Background(), r, func(p int) {
		if p < progress || p > 99 {
			t.Errorf("invalid progress %d", p)
		}
		progress = p
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if _, err := os.Stat(filepath.Join(bundle, "original")); err != nil {
		t.Fatal("running app changed")
	}
	if err := verifyUpdateBundle(context.Background(), filepath.Join(dir, "unpacked/DevJay.app"), "0.8.1"); err != nil {
		t.Fatal(err)
	}
	if err := verifyUpdateBundle(context.Background(), filepath.Join(dir, "unpacked/DevJay.app"), "0.8.2"); err == nil {
		t.Fatal("wrong version accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := u.download(ctx, r, func(int) {}); err == nil {
		t.Fatal("cancelled download accepted")
	}
}
