package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Git has no permanent branch ID. A surviving creation entry identifies this
// local incarnation; a later ordinary reflog entry must not masquerade as birth.
func parseBranchCreation(line string) (string, time.Time) {
	header, message, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
	fields := strings.Fields(header)
	if !ok || len(fields) < 5 || !strings.HasPrefix(message, "branch: Created from ") {
		return "", time.Time{}
	}
	if (len(fields[0]) != 40 && len(fields[0]) != 64) || strings.Trim(fields[0], "0") != "" || len(fields[1]) != len(fields[0]) {
		return "", time.Time{}
	}
	seconds, err := strconv.ParseInt(fields[len(fields)-2], 10, 64)
	if err != nil || seconds <= 0 {
		return "", time.Time{}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(line))), time.Unix(seconds, 0)
}

func branchCreation(ctx context.Context, path, branch string) (string, time.Time) {
	logPath := gitString(ctx, path, "rev-parse", "--git-path", "logs/refs/heads/"+branch)
	if logPath == "" {
		return "", time.Time{}
	}
	if !filepath.IsAbs(logPath) {
		logPath = filepath.Join(path, logPath)
	}
	file, err := os.Open(logPath)
	if err != nil {
		return "", time.Time{}
	}
	defer file.Close()
	// Only the first record is needed, even on long-lived branches. Git resolves
	// the path so linked worktrees use the common repository's branch reflog.
	line, err := bufio.NewReader(io.LimitReader(file, 16<<10)).ReadString('\n')
	if err != nil {
		return "", time.Time{}
	}
	return parseBranchCreation(line)
}
