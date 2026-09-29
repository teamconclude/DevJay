package main

import (
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const claudeWaitingMaxBytes = 16 * 1024

// This is the small process registry written by Claude Code, not its session
// transcript. Decode only identity and state; names, messages, socket addresses,
// and other registry fields are deliberately ignored. The registry is an
// observed implementation detail, so missing or unfamiliar data is unknown.
type claudeWaitingRecord struct {
	PID             int    `json:"pid"`
	PIDDomain       string `json:"pidDomain"`
	ProcStart       string `json:"procStart"`
	Cwd             string `json:"cwd"`
	Status          string `json:"status"`
	StartedAt       int64  `json:"startedAt"`
	UpdatedAt       int64  `json:"updatedAt"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
}

// Called on the background scanner's newly collected process slice. Only
// currently observed Claude PIDs are read; stale registry files are never
// enumerated. Waiting and Working are positive observations; neither false
// value proves the opposite state or that no user action is needed.
func applyClaudeWaiting(processes []observedAgentProcess, sessionsDir string, now time.Time) {
	hasClaude := false
	for i := range processes {
		if processes[i].Name == "Claude Code" {
			processes[i].Waiting = false
			processes[i].Working = false
			hasClaude = true
		}
	}
	if !hasClaude || !filepath.IsAbs(sessionsDir) || now.IsZero() {
		return
	}
	// Anchor all reads to this directory descriptor. Refuse a substituted
	// symlink directory, symlink files, and nonregular files (including FIFOs).
	directory, err := unix.Open(sessionsDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return
	}
	defer unix.Close(directory)
	var directoryInfo unix.Stat_t
	if unix.Fstat(directory, &directoryInfo) != nil || directoryInfo.Uid != uint32(os.Getuid()) {
		return
	}
	resolved := make(map[string]string)
	canonical := func(path string) string {
		if value, ok := resolved[path]; ok {
			return value
		}
		value := ""
		if filepath.IsAbs(path) {
			if real, err := filepath.EvalSymlinks(path); err == nil {
				value = filepath.Clean(real)
			}
		}
		resolved[path] = value
		return value
	}
	records := make(map[int]*claudeWaitingRecord)
	for i := range processes {
		process := &processes[i]
		if process.Name != "Claude Code" || process.PID <= 1 || process.Started == 0 || process.Started > math.MaxInt64 {
			continue
		}
		record, read := records[process.PID]
		if !read {
			record = readClaudeWaitingRecord(directory, process.PID)
			records[process.PID] = record
		}
		if record == nil || !record.matches(*process, now) {
			continue
		}
		cwd := canonical(process.Cwd)
		if cwd == "" || cwd != canonical(record.Cwd) {
			continue
		}
		process.Waiting = record.Status == "waiting"
		// Claude 2.1.283/284 uses busy for loading or delegated agent work.
		// shell is idle agent state with unfinished local shell tasks, which
		// can be a persistent development server rather than agent work.
		process.Working = record.Status == "busy"
	}
}

func readClaudeWaitingRecord(directory, pid int) *claudeWaitingRecord {
	fd, err := unix.Openat(directory, strconv.Itoa(pid)+".json", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	file := os.NewFile(uintptr(fd), "Claude process metadata")
	defer file.Close()
	var info unix.Stat_t
	if unix.Fstat(fd, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFREG ||
		info.Uid != uint32(os.Getuid()) || info.Size <= 0 || info.Size > claudeWaitingMaxBytes {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(file, claudeWaitingMaxBytes+1))
	if err != nil || len(data) > claudeWaitingMaxBytes {
		return nil
	}
	var record claudeWaitingRecord
	if json.Unmarshal(data, &record) != nil {
		return nil
	}
	return &record
}

func (r claudeWaitingRecord) matches(process observedAgentProcess, now time.Time) bool {
	if r.PID != process.PID || r.PIDDomain != "darwin" {
		return false
	}
	switch r.Status {
	case "busy", "shell", "idle", "waiting":
	default:
		return false
	}
	// Claude 2.1.283 writes `LC_ALL=C TZ=UTC ps -o lstart=` as its process
	// identity. Its precision is seconds; startedAt must also postdate this
	// kernel process to reject PID reuse within the same second.
	started := time.UnixMicro(int64(process.Started)).UTC()
	stamp := strings.Join(strings.Fields(r.ProcStart), " ")
	want := strings.Join(strings.Fields(started.Format("Mon Jan _2 15:04:05 2006")), " ")
	if stamp != want || r.StartedAt < started.UnixMilli() || r.StartedAt <= 0 ||
		r.StatusUpdatedAt < r.StartedAt || r.UpdatedAt < r.StatusUpdatedAt ||
		r.UpdatedAt > now.Add(time.Minute).UnixMilli() {
		return false
	}
	// These timestamps advance on changes, not on a heartbeat. A long-lived
	// waiting dialog stays valid while this exact process is still observed.
	return true
}
