package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
)

// Only these metadata fields survive a scan. Thread IDs are used locally to
// read status, never attached to Site snapshots or retained between scans.
type codexWaitingThread struct {
	Cwd     string
	Source  string
	Waiting bool
	Working bool
}

const (
	codexWaitingThreadLimit  = 64
	codexWaitingPageLimit    = 4
	codexWaitingMessageSize  = 256 * 1024
	codexWaitingMessageLimit = 128
	codexWaitingByteLimit    = 2 * 1024 * 1024
)

// Codex's default control path is an owned symlink into a private temporary
// directory. Follow a bounded chain of owned links, then require a private,
// owned Unix socket. No endpoint URL, network fallback, or auth file is used.
func codexWaitingSocket(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("Codex control socket path must be absolute")
	}
	for links := 0; links <= 8; links++ {
		info, err := os.Lstat(path)
		if err != nil {
			return "", errors.New("Codex control socket unavailable")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Getuid()) {
			return "", errors.New("Codex control socket has an unexpected owner")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return "", errors.New("Codex control socket link unavailable")
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			path = filepath.Clean(target)
			continue
		}
		if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm()&0077 != 0 {
			return "", errors.New("Codex control endpoint is not a private socket")
		}
		return path, nil
	}
	return "", errors.New("Codex control socket has too many links")
}

type codexWaitingRPC struct {
	connection *websocket.Conn
	sequence   int
	messages   int
	bytes      int
}

func (r *codexWaitingRPC) request(ctx context.Context, method string, params any, result any) error {
	// Keep the observer's complete capability set visible and read-only.
	if method != "initialize" && method != "thread/loaded/list" && method != "thread/read" {
		return errors.New("unsupported Codex observer request")
	}
	r.sequence++
	payload, err := json.Marshal(struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{r.sequence, method, params})
	if err != nil {
		return err
	}
	if err := r.connection.Write(ctx, websocket.MessageText, payload); err != nil {
		return errors.New("Codex status request failed")
	}
	for r.messages < codexWaitingMessageLimit {
		kind, payload, err := r.connection.Read(ctx)
		if err != nil {
			return errors.New("Codex status response unavailable")
		}
		r.messages++
		r.bytes += len(payload)
		if kind != websocket.MessageText || r.bytes > codexWaitingByteLimit {
			return errors.New("Codex status response exceeds limits")
		}
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(payload, &envelope) != nil {
			return errors.New("invalid Codex status response")
		}
		// Notifications and server-initiated requests are never answered,
		// including approvals that happen to reuse one of our request IDs.
		if envelope.Method != "" {
			continue
		}
		var id int
		if json.Unmarshal(envelope.ID, &id) != nil || id != r.sequence {
			continue
		}
		if envelope.Error != nil {
			return fmt.Errorf("Codex status request rejected (%d)", envelope.Error.Code)
		}
		if len(envelope.Result) == 0 || string(envelope.Result) == "null" ||
			json.Unmarshal(envelope.Result, result) != nil {
			return errors.New("invalid Codex status result")
		}
		return nil
	}
	return errors.New("Codex status message limit exceeded")
}

type codexWaitingMetadata struct {
	Cwd    string          `json:"cwd"`
	Source json.RawMessage `json:"source"`
	Status struct {
		Type        string   `json:"type"`
		ActiveFlags []string `json:"activeFlags"`
	} `json:"status"`
}

func codexWaitingObservation(metadata codexWaitingMetadata) (codexWaitingThread, bool) {
	var source string
	if json.Unmarshal(metadata.Source, &source) != nil || (source != "cli" && source != "vscode") ||
		!filepath.IsAbs(metadata.Cwd) || len(metadata.Cwd) > 4096 || !utf8.ValidString(metadata.Cwd) {
		return codexWaitingThread{}, false
	}
	result := codexWaitingThread{Cwd: metadata.Cwd, Source: source}
	switch metadata.Status.Type {
	case "active":
		// The known schema requires activeFlags, even when empty. Missing,
		// null, or new flags cannot prove that the agent is processing.
		result.Working = metadata.Status.ActiveFlags != nil && len(metadata.Status.ActiveFlags) == 0
		for _, flag := range metadata.Status.ActiveFlags {
			if flag == "waitingOnApproval" || flag == "waitingOnUserInput" {
				result.Waiting = true
			}
		}
	case "idle", "notLoaded", "systemError":
	default:
		return codexWaitingThread{}, false
	}
	return result, true
}

// Query an already running daemon. This never starts a server, subscribes to or
// resumes a thread, answers a prompt, or changes Codex configuration. Runtime
// status is authoritative only for this daemon's loaded CLI/IDE sessions.
func readCodexWaiting(ctx context.Context, socketPath string) ([]codexWaitingThread, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	path, err := codexWaitingSocket(socketPath)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", path)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	connection, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		// Transport errors can include server-controlled handshake bodies.
		return nil, errors.New("Codex control connection unavailable")
	}
	defer connection.CloseNow()
	connection.SetReadLimit(codexWaitingMessageSize)
	rpc := codexWaitingRPC{connection: connection}
	var initialized struct{}
	if err := rpc.request(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "devjay_status_observer", "title": "DevJay", "version": "1"},
		"capabilities": map[string]bool{"experimentalApi": false},
	}, &initialized); err != nil {
		return nil, err
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"method":"initialized","params":{}}`)); err != nil {
		return nil, errors.New("Codex control initialization unavailable")
	}
	ids := make([]string, 0)
	seenIDs, seenCursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	for page := 0; ; page++ {
		if page >= codexWaitingPageLimit {
			return nil, errors.New("Codex loaded thread page limit exceeded")
		}
		params := map[string]any{"limit": codexWaitingThreadLimit}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var loaded struct {
			Data       []string `json:"data"`
			NextCursor *string  `json:"nextCursor"`
		}
		if err := rpc.request(ctx, "thread/loaded/list", params, &loaded); err != nil {
			return nil, err
		}
		if loaded.Data == nil {
			return nil, errors.New("invalid Codex loaded thread list")
		}
		for _, id := range loaded.Data {
			if id == "" || len(id) > 256 {
				return nil, errors.New("invalid Codex loaded thread identifier")
			}
			if !seenIDs[id] {
				seenIDs[id] = true
				ids = append(ids, id)
			}
		}
		if len(ids) > codexWaitingThreadLimit {
			return nil, errors.New("Codex loaded thread limit exceeded")
		}
		if loaded.NextCursor == nil {
			break
		}
		cursor = *loaded.NextCursor
		if cursor == "" || len(cursor) > 1024 || seenCursors[cursor] {
			return nil, errors.New("invalid Codex loaded thread cursor")
		}
		seenCursors[cursor] = true
	}
	results := make([]codexWaitingThread, 0, len(ids))
	for _, id := range ids {
		var response struct {
			Thread codexWaitingMetadata `json:"thread"`
		}
		if err := rpc.request(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &response); err != nil {
			return nil, err
		}
		if observation, ok := codexWaitingObservation(response.Thread); ok {
			results = append(results, observation)
		}
	}
	return results, nil
}
