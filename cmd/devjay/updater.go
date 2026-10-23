package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var buildVersion = "0.0.0"

const releaseURL = "https://github.com/teamconclude/DevJay/releases"

var releaseVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type UpdateStatus struct {
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
	Percent int    `json:"percent,omitempty"`
	Message string `json:"message,omitempty"`
}
type updateRelease struct{ version, url, checksum string }
type appUpdater struct {
	client                  *http.Client
	bundle, staged, version string
}

func newerRelease(a, b string) bool {
	if !releaseVersionPattern.MatchString(a) || !releaseVersionPattern.MatchString(b) {
		return false
	}
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range aa {
		if len(aa[i]) != len(bb[i]) {
			return len(aa[i]) > len(bb[i])
		}
		if aa[i] != bb[i] {
			return aa[i] > bb[i]
		}
	}
	return false
}
func updateHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 10 {
			return errors.New("unsafe update redirect")
		}
		return nil
	}}
}
func (u *appUpdater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	res, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Update server returned HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("Update metadata is too large")
	}
	return data, err
}
func (u *appUpdater) latest(ctx context.Context, base string) (*updateRelease, error) {
	data, err := u.get(ctx, base+"/latest/download/version.txt", 128)
	if err != nil {
		return nil, err
	}
	version := strings.TrimSpace(string(data))
	if !releaseVersionPattern.MatchString(version) {
		return nil, errors.New("Invalid release version")
	}
	if !newerRelease(version, u.version) {
		return nil, nil
	}
	name := "DevJay-" + version + "-macos-universal.zip"
	url := base + "/download/v" + version + "/" + name
	data, err = u.get(ctx, url+".sha256", 512)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || fields[1] != name {
		return nil, errors.New("Invalid release checksum")
	}
	hash, err := hex.DecodeString(fields[0])
	if err != nil || len(hash) != sha256.Size {
		return nil, errors.New("Invalid release checksum")
	}
	return &updateRelease{version, url, strings.ToLower(fields[0])}, nil
}
func (u *appUpdater) installProblem() error {
	if !strings.HasSuffix(u.bundle, ".app") || strings.Contains(u.bundle, "/AppTranslocation/") || strings.HasPrefix(u.bundle, "/Volumes/") {
		return errors.New("Move DevJay to Applications before updating")
	}
	info, err := os.Lstat(u.bundle)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("The application bundle cannot be replaced")
	}
	probe, err := os.MkdirTemp(filepath.Dir(u.bundle), ".devjay-write-test-")
	if err != nil {
		return errors.New("The application folder is not writable; download the update manually")
	}
	return os.Remove(probe)
}
func updateCommand(ctx context.Context, name string, args ...string) error {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %.1000s", filepath.Base(name), err, output)
	}
	return nil
}
func verifyUpdateBundle(ctx context.Context, bundle, version string) error {
	if err := updateCommand(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", bundle); err != nil {
		return err
	}
	for key, want := range map[string]string{"CFBundleIdentifier": "io.teamconclude.devjay", "CFBundleShortVersionString": version} {
		out, err := exec.CommandContext(ctx, "/usr/libexec/PlistBuddy", "-c", "Print :"+key, filepath.Join(bundle, "Contents/Info.plist")).Output()
		if err != nil || strings.TrimSpace(string(out)) != want {
			return errors.New("Downloaded application identity or version does not match the release")
		}
	}
	return nil
}
func (u *appUpdater) download(ctx context.Context, r *updateRelease, progress func(int)) (string, error) {
	if err := u.installProblem(); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(filepath.Dir(u.bundle), ".devjay-update-")
	if err != nil {
		return "", err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(dir)
		}
	}()
	req, err := http.NewRequestWithContext(ctx, "GET", r.url, nil)
	if err != nil {
		return "", err
	}
	res, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("Download returned HTTP %d", res.StatusCode)
	}
	const maximum = int64(1 << 30)
	if res.ContentLength > maximum {
		return "", errors.New("Update archive is too large")
	}
	file, err := os.OpenFile(filepath.Join(dir, "update.zip"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	buf := make([]byte, 128*1024)
	var received int64
	last := -1
	for {
		n, e := res.Body.Read(buf)
		if n > 0 {
			received += int64(n)
			if received > maximum {
				err = errors.New("Update archive is too large")
				break
			}
			if _, err = file.Write(buf[:n]); err != nil {
				break
			}
			hash.Write(buf[:n])
			percent := 0
			if res.ContentLength > 0 {
				percent = int(received * 100 / res.ContentLength)
				if percent > 99 {
					percent = 99
				}
			}
			if percent != last {
				progress(percent)
				last = percent
			}
		}
		if e != nil {
			if e != io.EOF {
				err = e
			}
			break
		}
	}
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if hex.EncodeToString(hash.Sum(nil)) != r.checksum {
		return "", errors.New("The download did not match the release checksum")
	}
	if err = validateUpdateArchive(filepath.Join(dir, "update.zip")); err != nil {
		return "", err
	}
	if err = updateCommand(ctx, "/usr/bin/ditto", "-x", "-k", filepath.Join(dir, "update.zip"), filepath.Join(dir, "unpacked")); err != nil {
		return "", err
	}
	if err = verifyUpdateBundle(ctx, filepath.Join(dir, "unpacked/DevJay.app"), r.version); err != nil {
		return "", err
	}
	success = true
	return dir, nil
}
func updaterBundle() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(exe), "../.."))
}

// Kept separate for helper tests and argument-only shell invocation.
func updatePID() string { return strconv.Itoa(os.Getpid()) }

func validateUpdateArchive(filename string) error {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer z.Close()
	var size uint64
	for _, f := range z.File {
		name := path.Clean(f.Name)
		// ditto may include Apple resource-fork metadata alongside the app.
		if name != "DevJay.app" && !strings.HasPrefix(name, "DevJay.app/") && name != "__MACOSX" && !strings.HasPrefix(name, "__MACOSX/") {
			return errors.New("Unexpected path in update archive")
		}
		if strings.Contains(f.Name, "../") || strings.HasPrefix(f.Name, "/") || f.Mode()&os.ModeSymlink != 0 {
			return errors.New("Unsafe path in update archive")
		}
		if f.UncompressedSize64 > 2<<30 || size > (2<<30)-f.UncompressedSize64 {
			return errors.New("Expanded update is too large")
		}
		size += f.UncompressedSize64
	}
	return nil
}
