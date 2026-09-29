package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type codexTestRequest struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

func codexTestRead(ctx context.Context, connection *websocket.Conn) (codexTestRequest, error) {
	_, payload, err := connection.Read(ctx)
	var request codexTestRequest
	if err == nil {
		err = json.Unmarshal(payload, &request)
	}
	return request, err
}

func codexTestSend(ctx context.Context, connection *websocket.Conn, value any) error {
	data, err := json.Marshal(value)
	if err == nil {
		err = connection.Write(ctx, websocket.MessageText, data)
	}
	return err
}

func codexTestSocket(t *testing.T, handle func(context.Context, *websocket.Conn)) string {
	t.Helper()
	// Keep below macOS's short Unix-socket path limit, even when TMPDIR is long.
	dir, err := os.MkdirTemp("/tmp", "devjay-codex-")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "control.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		os.RemoveAll(dir)
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	started, done := make(chan struct{}), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		connection, err := websocket.Accept(w, request, nil)
		if err != nil {
			t.Errorf("accept test observer: %v", err)
			return
		}
		close(started)
		defer close(done)
		defer connection.CloseNow()
		handle(ctx, connection)
	})}
	go server.Serve(listener)
	t.Cleanup(func() {
		cancel()
		server.Close()
		select {
		case <-started:
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("test observer did not close")
			}
		default:
		}
		os.RemoveAll(dir)
	})
	return path
}

func codexTestHandshake(t *testing.T, ctx context.Context, connection *websocket.Conn) bool {
	t.Helper()
	request, err := codexTestRead(ctx, connection)
	if err != nil || request.Method != "initialize" {
		t.Errorf("expected initialize: %+v %v", request, err)
		return false
	}
	if err := codexTestSend(ctx, connection, map[string]any{"id": request.ID, "result": map[string]any{}}); err != nil {
		t.Error(err)
		return false
	}
	request, err = codexTestRead(ctx, connection)
	if err != nil || request.Method != "initialized" || request.ID != 0 {
		t.Errorf("expected initialized notification: %+v %v", request, err)
		return false
	}
	return true
}

func TestCodexWaitingReadsMetadataWithoutAnsweringServerRequests(t *testing.T) {
	path := codexTestSocket(t, func(ctx context.Context, connection *websocket.Conn) {
		request, err := codexTestRead(ctx, connection)
		if err != nil || request.Method != "initialize" {
			t.Errorf("expected initialize: %+v %v", request, err)
			return
		}
		// A server request using our ID must not be mistaken for a response or
		// answered. Neither unrelated response bodies nor prompts are retained.
		codexTestSend(ctx, connection, map[string]any{"id": request.ID, "method": "item/commandExecution/requestApproval", "params": map[string]any{"reason": "private approval text"}})
		codexTestSend(ctx, connection, map[string]any{"id": 999, "result": "unrelated private result"})
		codexTestSend(ctx, connection, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "private message"}})
		codexTestSend(ctx, connection, map[string]any{"id": request.ID, "result": map[string]any{}})
		request, err = codexTestRead(ctx, connection)
		if err != nil || request.Method != "initialized" {
			t.Errorf("observer answered a server request: %+v %v", request, err)
			return
		}
		for index, method := range []string{"thread/loaded/list", "thread/read", "thread/read", "thread/read"} {
			request, err = codexTestRead(ctx, connection)
			if err != nil || request.Method != method {
				t.Errorf("unexpected observer method: %+v %v", request, err)
				return
			}
			var result any
			if index == 0 {
				result = map[string]any{"data": []string{"waiting", "idle", "working"}, "nextCursor": nil}
			} else {
				if request.Params["includeTurns"] != false {
					t.Error("observer requested conversation history")
				}
				status := map[string]any{"type": "idle"}
				if index == 1 {
					status = map[string]any{"type": "active", "activeFlags": []string{"waitingOnUserInput"}}
				} else if index == 3 {
					status = map[string]any{"type": "active", "activeFlags": []string{}}
				}
				result = map[string]any{"thread": map[string]any{"id": request.Params["threadId"], "cwd": "/workspace", "source": "vscode", "status": status, "preview": "private thread preview"}}
			}
			if err := codexTestSend(ctx, connection, map[string]any{"id": request.ID, "result": result}); err != nil {
				t.Error(err)
				return
			}
		}
		if request, err := codexTestRead(ctx, connection); err == nil {
			t.Errorf("unexpected message after metadata reads: %+v", request)
		}
	})
	link := filepath.Join(filepath.Dir(path), "default.sock")
	if err := os.Symlink(filepath.Base(path), link); err != nil {
		t.Fatal(err)
	}
	got, err := readCodexWaiting(context.Background(), link)
	want := []codexWaitingThread{{Cwd: "/workspace", Source: "vscode", Waiting: true}, {Cwd: "/workspace", Source: "vscode"}, {Cwd: "/workspace", Source: "vscode", Working: true}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata results: %+v %v", got, err)
	}
	serialized, _ := json.Marshal(got)
	if strings.Contains(string(serialized), "private") || strings.Contains(string(serialized), "threadId") {
		t.Fatal("observer retained session content or identifiers")
	}
}

func TestCodexWaitingStatusAndSourceCoverage(t *testing.T) {
	for _, source := range []string{`"cli"`, `"vscode"`, `"exec"`, `"appServer"`, `"unknown"`, `{"custom":"desktop"}`, `{"subAgent":{}}`, `null`} {
		for _, status := range []struct {
			kind    string
			flags   []string
			waiting bool
			working bool
		}{
			{"active", []string{"waitingOnApproval"}, true, false},
			{"active", []string{"waitingOnUserInput"}, true, false},
			{"active", []string{"other"}, false, false},
			{"active", []string{"other", "waitingOnApproval"}, true, false},
			{"active", []string{}, false, true},
			{"active", nil, false, false},
			{"idle", []string{"waitingOnApproval"}, false, false},
			{"idle", []string{}, false, false},
			{"notLoaded", nil, false, false},
			{"systemError", nil, false, false},
			{"futureStatus", []string{"waitingOnApproval"}, false, false},
		} {
			metadata := codexWaitingMetadata{Cwd: "/workspace", Source: json.RawMessage(source)}
			metadata.Status.Type, metadata.Status.ActiveFlags = status.kind, status.flags
			got, ok := codexWaitingObservation(metadata)
			wantKnown := (source == `"cli"` || source == `"vscode"`) && status.kind != "futureStatus"
			if ok != wantKnown || (ok && (got.Waiting != status.waiting || got.Working != status.working)) {
				t.Fatalf("source=%s status=%+v: %+v %v", source, status, got, ok)
			}
		}
	}
	for _, cwd := range []string{"", "relative", "/" + strings.Repeat("x", 4096), "/bad\xff"} {
		metadata := codexWaitingMetadata{Cwd: cwd, Source: json.RawMessage(`"cli"`)}
		metadata.Status.Type = "idle"
		if _, ok := codexWaitingObservation(metadata); ok {
			t.Fatalf("invalid working directory accepted: %q", cwd)
		}
	}
}

func TestCodexWorkingRequiresAnExplicitEmptyFlagsArray(t *testing.T) {
	for _, test := range []struct {
		status  string
		working bool
	}{
		{`{"type":"active","activeFlags":[]}`, true},
		{`{"type":"active"}`, false},
		{`{"type":"active","activeFlags":null}`, false},
		{`{"type":"active","activeFlags":[null]}`, false},
		{`{"type":"active","activeFlags":["futureBlocker"]}`, false},
		{`{"type":"active","activeFlags":["waitingOnApproval"]}`, false},
		{`{"type":"idle","activeFlags":[]}`, false},
	} {
		var metadata codexWaitingMetadata
		if err := json.Unmarshal([]byte(`{"cwd":"/workspace","source":"cli","status":`+test.status+`}`), &metadata); err != nil {
			t.Fatal(err)
		}
		got, ok := codexWaitingObservation(metadata)
		if !ok || got.Working != test.working {
			t.Fatalf("%s: working=%v accepted=%v", test.status, got.Working, ok)
		}
	}
}

func TestCodexWaitingRejectsUnsafeSocketPaths(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ordinary-file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	dangling, cycle := filepath.Join(dir, "dangling"), filepath.Join(dir, "cycle")
	if err := os.Symlink("missing", dangling); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("cycle", cycle); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative.sock", file, dangling, cycle} {
		if _, err := codexWaitingSocket(path); err == nil {
			t.Fatalf("unsafe socket accepted: %s", path)
		}
	}
	if os.Getuid() != 0 {
		if _, err := codexWaitingSocket("/bin/sh"); err == nil || !strings.Contains(err.Error(), "owner") {
			t.Fatalf("root-owned endpoint accepted: %v", err)
		}
	}
	path := codexTestSocket(t, func(context.Context, *websocket.Conn) { t.Error("unsafe socket was contacted") })
	if err := os.Chmod(path, 0660); err != nil {
		t.Fatal(err)
	}
	if _, err := readCodexWaiting(context.Background(), path); err == nil {
		t.Fatal("group-readable socket accepted")
	}
}

func TestCodexWaitingRejectsMalformedOversizedAndTimedOutResponses(t *testing.T) {
	for _, mode := range []string{"malformed", "missing-result", "oversized", "error", "timeout", "message-limit"} {
		t.Run(mode, func(t *testing.T) {
			path := codexTestSocket(t, func(ctx context.Context, connection *websocket.Conn) {
				request, err := codexTestRead(ctx, connection)
				if err != nil {
					return
				}
				switch mode {
				case "malformed":
					connection.Write(ctx, websocket.MessageText, []byte("{"))
				case "missing-result":
					codexTestSend(ctx, connection, map[string]any{"id": request.ID})
				case "oversized":
					connection.Write(ctx, websocket.MessageText, []byte(strings.Repeat("x", codexWaitingMessageSize+1)))
				case "error":
					codexTestSend(ctx, connection, map[string]any{"id": request.ID, "error": map[string]any{"code": -1, "message": "private error details"}})
				case "timeout":
					codexTestRead(ctx, connection)
				case "message-limit":
					for i := 0; i < codexWaitingMessageLimit; i++ {
						if codexTestSend(ctx, connection, map[string]any{"method": "notification", "params": map[string]any{}}) != nil {
							break
						}
					}
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			if records, err := readCodexWaiting(ctx, path); err == nil || len(records) != 0 || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe response accepted/retained: %+v %v", records, err)
			}
		})
	}
}

func TestCodexWaitingBoundsLoadedThreadPagination(t *testing.T) {
	for _, mode := range []string{"pages", "duplicate-cursor", "threads", "missing-data"} {
		t.Run(mode, func(t *testing.T) {
			path := codexTestSocket(t, func(ctx context.Context, connection *websocket.Conn) {
				if !codexTestHandshake(t, ctx, connection) {
					return
				}
				for page := 0; ; page++ {
					request, err := codexTestRead(ctx, connection)
					if err != nil {
						return
					}
					if request.Method != "thread/loaded/list" {
						t.Errorf("overflow caused metadata read: %+v", request)
						return
					}
					result := map[string]any{"data": []string{}, "nextCursor": fmt.Sprint(page)}
					switch mode {
					case "duplicate-cursor":
						result["nextCursor"] = "same"
					case "threads":
						ids := make([]string, codexWaitingThreadLimit+1)
						for i := range ids {
							ids[i] = fmt.Sprint(i)
						}
						result["data"], result["nextCursor"] = ids, nil
					case "missing-data":
						delete(result, "data")
					}
					if codexTestSend(ctx, connection, map[string]any{"id": request.ID, "result": result}) != nil {
						return
					}
				}
			})
			if records, err := readCodexWaiting(context.Background(), path); err == nil || records != nil {
				t.Fatalf("unbounded/incomplete list accepted: %+v %v", records, err)
			}
		})
	}
}
