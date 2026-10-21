package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const claudePermissionLimit = 64 * 1024

// Human approvals may wait through meetings or overnight. The live connection,
// cancellation and process/session revalidation remain the authority.
const claudePermissionTimeout = 24 * time.Hour
const claudePermissionReconnectGrace = 30 * time.Second
const claudeHookTimeoutSeconds = int(claudePermissionTimeout/time.Second) + 10

// This deliberately omits transcript paths, permission suggestions and rule
// changes. A response authorizes this invocation only; it never saves a rule.
type claudePermissionInput struct {
	Session string          `json:"session_id"`
	Cwd     string          `json:"cwd"`
	Event   string          `json:"hook_event_name"`
	Tool    string          `json:"tool_name"`
	Input   json.RawMessage `json:"tool_input"`
}

func (r claudePermissionInput) valid() bool {
	if r.Event != "PermissionRequest" || r.Session == "" || len(r.Session) > 128 || !filepath.IsAbs(r.Cwd) || len(r.Cwd) > 4096 {
		return false
	}
	// Interactive questions/plan dialogs are not equivalent to tool approvals.
	switch r.Tool {
	case "Bash", "Edit", "Write", "MultiEdit":
	default:
		return false
	}
	var object map[string]json.RawMessage
	return len(r.Input) > 0 && len(r.Input) <= claudePermissionLimit/2 && json.Unmarshal(r.Input, &object) == nil && object != nil
}

// Claude keeps its process/registry cwd at the session root, while hook cwd
// follows shell navigation. Authenticate the session against that root, then
// allow an existing working directory inside it. Resolve links before checking
// containment so a similarly named sibling or an escaping link is not accepted.
func claudePermissionSessionMatches(record *claudeWaitingRecord, process observedAgentProcess, input claudePermissionInput, now time.Time) bool {
	if record == nil || !record.matches(process, now) || record.SessionID != input.Session {
		return false
	}
	root := canonicalAgentDirectory(process.Cwd)
	cwd := canonicalAgentDirectory(input.Cwd)
	if root == "" || cwd == "" || canonicalAgentDirectory(record.Cwd) != root {
		return false
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return false
	}
	relative, err := filepath.Rel(root, cwd)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// Versioned only on our local socket; Claude never receives this envelope.
// Older helpers retain their single-response behavior during an app update.
type claudePermissionWireRequest struct {
	claudePermissionInput
	Protocol int `json:"devjay_protocol,omitempty"`
}

type claudePermissionWireResponse struct {
	State    string `json:"state,omitempty"`
	Decision string `json:"decision,omitempty"`
}

type claudePermissionRequest struct {
	Preview  *claudePermissionPreview `json:"preview,omitempty"`
	Token    string                   `json:"token"`
	Tool     string                   `json:"tool"`
	Details  string                   `json:"details"`
	Cwd      string                   `json:"cwd"`
	Session  string                   `json:"session"`
	Created  time.Time                `json:"-"`
	Process  observedAgentProcess     `json:"-"`
	peer     int
	conn     *net.UnixConn
	done     chan struct{}
	decision chan string
}
type claudePermissionServer struct {
	disabled bool // Protected by mu; rejects cached hooks after disabling.
	mu       sync.Mutex
	pending  map[string]*claudePermissionRequest
	changed  chan struct{}
	listener *net.UnixListener
	verify   func(int, claudePermissionInput) (observedAgentProcess, error)
}

func claudePermissionSocket() string {
	return filepath.Join("/tmp", fmt.Sprintf("devjay-%d-%s", os.Getuid(), configNamespace()), "claude.sock")
}
func privateClaudeSocketDirectory(path string, create bool) error {
	parent := filepath.Dir(path)
	if create {
		if err := os.Mkdir(parent, 0700); err != nil && !os.IsExist(err) {
			return err
		}
	}
	st, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || st.Mode().Perm() != 0700 || int(stat.Uid) != os.Getuid() {
		return errors.New("Claude bridge directory is not private")
	}
	return nil
}
func startClaudePermissionServer(ctx context.Context, path string, verify func(int, claudePermissionInput) (observedAgentProcess, error)) (*claudePermissionServer, error) {
	if err := privateClaudeSocketDirectory(path, true); err != nil {
		return nil, err
	}
	// Never unlink a live listener. Refuse any unexpected filesystem object.
	if st, err := os.Lstat(path); err == nil {
		stat, ok := st.Sys().(*syscall.Stat_t)
		if !ok || st.Mode()&os.ModeSocket == 0 || int(stat.Uid) != os.Getuid() {
			return nil, errors.New("Unexpected Claude bridge socket")
		}
		conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil, errors.New("Claude bridge already running")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	s := &claudePermissionServer{pending: map[string]*claudePermissionRequest{}, changed: make(chan struct{}, 1), listener: listener, verify: verify}
	// Closing the app is a transport interruption, not an explicit handoff.
	// Helpers may reconnect briefly; releaseAll is reserved for disabling.
	go func() { <-ctx.Done(); listener.Close() }()
	go func() {
		slots := make(chan struct{}, 16)
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			select {
			case slots <- struct{}{}:
				go func() { defer func() { <-slots }(); s.serve(ctx, conn) }()
			default:
				conn.Close()
			}
		}
	}()
	return s, nil
}
func (s *claudePermissionServer) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}
func (s *claudePermissionServer) list() []*claudePermissionRequest {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*claudePermissionRequest, 0, len(s.pending))
	for _, r := range s.pending {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}
func (s *claudePermissionServer) releaseAllLocked() {
	for token, r := range s.pending {
		select {
		case r.decision <- "fallback":
		default:
		}
		delete(s.pending, token)
	}
	s.notify()
}
func (s *claudePermissionServer) releaseAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseAllLocked()
}
func (s *claudePermissionServer) setEnabled(enabled bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disabled = !enabled
	if s.disabled {
		s.releaseAllLocked()
	}
}
func (s *claudePermissionServer) accepting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.disabled
}
func (s *claudePermissionServer) decide(token, decision string) bool {
	if s == nil || (decision != "allow" && decision != "deny" && decision != "fallback") {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.pending[token]
	if r == nil || s.disabled {
		return false
	}
	select {
	case <-r.done:
		return false
	default:
	}
	select {
	case r.decision <- decision:
		delete(s.pending, token)
		s.notify()
		return true
	default:
		return false
	}
}
func peerPID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid int
	var failure error
	err = raw.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if e != nil {
			failure = e
			return
		}
		if cred.Uid != uint32(os.Getuid()) {
			failure = errors.New("Wrong bridge user")
			return
		}
		pid, failure = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	})
	if err != nil {
		return 0, err
	}
	return pid, failure
}
func readPermissionLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadSlice('\n')
	if err != nil {
		return nil, err
	}
	if len(line) > claudePermissionLimit {
		return nil, errors.New("Request too large")
	}
	return line, nil
}
func (s *claudePermissionServer) serve(ctx context.Context, conn *net.UnixConn) {
	s.serveWithTimeout(ctx, conn, claudePermissionTimeout)
}

func (s *claudePermissionServer) serveWithTimeout(ctx context.Context, conn *net.UnixConn, lifetime time.Duration) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	peer, err := peerPID(conn)
	if err != nil {
		return
	}
	reader := bufio.NewReaderSize(conn, claudePermissionLimit+1)
	line, err := readPermissionLine(reader)
	if err != nil {
		return
	}
	var wire claudePermissionWireRequest
	if json.Unmarshal(line, &wire) != nil || !wire.valid() || (wire.Protocol != 0 && wire.Protocol != 1) {
		return
	}
	input := wire.claudePermissionInput
	if !s.accepting() || reader.Buffered() > 0 {
		return
	}
	process, err := s.verify(peer, input)
	if err != nil {
		return
	}
	var formatted bytes.Buffer
	if json.Indent(&formatted, input.Input, "", "  ") != nil {
		return
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return
	}
	preview := makeClaudePermissionPreview(input.Tool, formatted.String())
	if preview != nil && canonicalAgentDirectory(input.Cwd) != canonicalAgentDirectory(process.Cwd) {
		preview.Sections = append([]claudePermissionSection{{Label: "Working directory", Kind: "path", Text: safePermissionText(input.Cwd)}}, preview.Sections...)
	}
	r := &claudePermissionRequest{Preview: preview, Token: hex.EncodeToString(token[:]), Tool: input.Tool, Details: formatted.String(), Cwd: input.Cwd, Session: input.Session, Created: time.Now(), Process: process, peer: peer, conn: conn, done: make(chan struct{}), decision: make(chan string, 1)}
	// Acknowledgment distinguishes a dropped live request from rejection or an
	// older server. Only acknowledged requests may reconnect after EOF.
	if wire.Protocol == 1 {
		if json.NewEncoder(conn).Encode(claudePermissionWireResponse{State: "pending"}) != nil {
			return
		}
	}
	conn.SetDeadline(time.Time{})
	fallback := func() {
		if wire.Protocol == 1 {
			conn.SetWriteDeadline(time.Now().Add(time.Second))
			json.NewEncoder(conn).Encode(claudePermissionWireResponse{Decision: "fallback"})
		}
	}
	// EOF/interruption invalidates this one invocation. Any extra bytes are also
	// a protocol violation. No later prompt can inherit its response channel.
	go func() {
		var b [1]byte
		reader.Read(b[:])
		close(r.done)
		s.mu.Lock()
		delete(s.pending, r.Token)
		s.mu.Unlock()
		s.notify()
	}()
	s.mu.Lock()
	// Disabling may race with the slow initial identity check. Never publish
	// that in-flight request, even if it already received an acknowledgment.
	if s.disabled {
		s.mu.Unlock()
		fallback()
		return
	}
	s.pending[r.Token] = r
	s.mu.Unlock()
	s.notify()
	defer func() { s.mu.Lock(); delete(s.pending, r.Token); s.mu.Unlock(); s.notify() }()
	timeout := time.NewTimer(lifetime)
	defer timeout.Stop()
	// A live helper/socket is insufficient: Claude may have moved on without
	// terminating its hook child. Revalidate on a bounded background cadence.
	recheck := time.NewTicker(2 * time.Second)
	defer recheck.Stop()
	stillCurrent := func() bool {
		current, err := s.verify(peer, input)
		return err == nil && current.PID == process.PID && current.Started == process.Started && s.accepting()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.done:
			return
		case <-timeout.C:
			fallback()
			return
		case <-recheck.C:
			if !stillCurrent() {
				fallback()
				return
			}
		case decision := <-r.decision:
			if decision == "fallback" || !stillCurrent() {
				fallback()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-r.done:
				return
			default:
			}
			conn.SetWriteDeadline(time.Now().Add(time.Second))
			json.NewEncoder(conn).Encode(claudePermissionWireResponse{Decision: decision})
			return
		}
	}
}

// This runs BEFORE Wails initialization in the hook subprocess. The original
// invocation stays in memory across a short app restart; it is never replayed
// after the helper exits. Failure/timeout leaves Claude's ordinary prompt.
func runClaudePermissionHook(ctx context.Context, input io.Reader, output io.Writer, path string) {
	runClaudePermissionHookWithTiming(ctx, input, output, path, claudePermissionTimeout, claudePermissionReconnectGrace, 250*time.Millisecond)
}

func runClaudePermissionHookWithTiming(ctx context.Context, input io.Reader, output io.Writer, path string, lifetime, grace, retryInterval time.Duration) {
	body, err := io.ReadAll(io.LimitReader(input, claudePermissionLimit+1))
	if err != nil || len(body) > claudePermissionLimit {
		return
	}
	var request claudePermissionInput
	if json.Unmarshal(body, &request) != nil || !request.valid() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, lifetime)
	defer cancel()
	deadline, _ := ctx.Deadline()
	wire := claudePermissionWireRequest{claudePermissionInput: request, Protocol: 1}
	var retryUntil time.Time
	for ctx.Err() == nil {
		if !retryUntil.IsZero() && !time.Now().Before(retryUntil) {
			return
		}
		conn, err := dialClaudePermissionSocket(ctx, path)
		if err != nil {
			// No startup wait: only a previously acknowledged request can retry.
			if retryUntil.IsZero() || (!os.IsNotExist(err) && !errors.Is(err, syscall.ECONNREFUSED)) {
				return
			}
		} else {
			connectionDeadline := deadline
			if !retryUntil.IsZero() && retryUntil.Before(connectionDeadline) {
				connectionDeadline = retryUntil
			}
			decision, disconnected := awaitClaudePermissionDecision(ctx, conn, wire, connectionDeadline, deadline)
			if ctx.Err() != nil {
				return
			}
			if decision == "allow" || decision == "deny" {
				json.NewEncoder(output).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PermissionRequest", "decision": map[string]string{"behavior": decision}}})
				return
			}
			if !disconnected {
				return
			}
			retryUntil = time.Now().Add(grace)
		}
		wait := retryInterval
		if remaining := time.Until(retryUntil); remaining < wait {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func dialClaudePermissionSocket(ctx context.Context, path string) (net.Conn, error) {
	if err := privateClaudeSocketDirectory(path, false); err != nil {
		return nil, err
	}
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || st.Mode()&os.ModeSocket == 0 || int(stat.Uid) != os.Getuid() || st.Mode().Perm() != 0600 {
		return nil, errors.New("Unexpected Claude bridge socket")
	}
	return (&net.Dialer{Timeout: 300 * time.Millisecond}).DialContext(ctx, "unix", path)
}

func awaitClaudePermissionDecision(ctx context.Context, conn net.Conn, request claudePermissionWireRequest, connectionDeadline, lifetimeDeadline time.Time) (decision string, disconnected bool) {
	defer conn.Close()
	conn.SetDeadline(connectionDeadline)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if json.NewEncoder(conn).Encode(request) != nil {
		return "", false
	}
	decoder := json.NewDecoder(io.LimitReader(conn, 256))
	var result claudePermissionWireResponse
	if decoder.Decode(&result) != nil {
		return "", false
	}
	if result.State == "pending" && result.Decision == "" {
		conn.SetDeadline(lifetimeDeadline)
		result = claudePermissionWireResponse{}
		if err := decoder.Decode(&result); err != nil {
			// Malformed replies are terminal. Only a transport loss on an
			// acknowledged invocation is eligible for a fresh connection/token.
			return "", errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET)
		}
	}
	if result.State != "" {
		return "", false
	}
	switch result.Decision {
	case "allow", "deny", "fallback":
		return result.Decision, false
	default:
		return "", false
	}
}
func claudePermissionHookMode() bool {
	if len(os.Args) != 2 || os.Args[1] != "--claude-permission-hook" {
		return false
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	runClaudePermissionHook(ctx, os.Stdin, os.Stdout, claudePermissionSocket())
	return true
}
func permissionHostKey(site, bundle string) string { return site + "\n" + bundle }
func safePermissionText(text string) string {
	// Keep control characters visible as escapes, including bidi controls that
	// could otherwise disguise command text. Never silently remove arguments.
	var out strings.Builder
	for _, r := range text {
		if (r < 32 && r != '\n' && r != '\t') || r == 127 || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			fmt.Fprintf(&out, "\\u%04x", r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}
