package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBrowserNavigationFallbackRequiresExplicitNoActionResult(t *testing.T) {
	const url = "https://github.com/team/project/pull/25"
	for _, test := range []struct {
		name, result string
		runError     error
		openError    error
		newTab       bool
		wantFallback bool
		wantError    bool
		wantMessage  string
	}{
		{name: "activated", result: `{"handled":true}`},
		{name: "no supported browser", result: `{"handled":false}`, wantFallback: true},
		{name: "denied", result: `{"handled":false,"unavailable":true}`, wantFallback: true, wantMessage: "Automation"},
		{name: "new tab fallback", result: `{"handled":false}`, newTab: true, wantFallback: true, wantMessage: "settings decide"},
		{name: "fallback failed", result: `{"handled":false}`, openError: errors.New("failed"), wantFallback: true, wantError: true},
		{name: "uncertain navigation", result: `{"error":"Check the browser before trying again."}`, wantError: true},
		{name: "timed out", runError: context.DeadlineExceeded, wantError: true},
		{name: "interrupted", runError: context.Canceled, wantError: true},
		{name: "unexpected output", result: `unexpected`, wantError: true},
		{name: "empty object", result: `{}`, wantError: true},
		{name: "null object", result: `null`, wantError: true},
		{name: "null result", result: `{"handled":null}`, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			message, err := openPullRequestBrowserWith(context.Background(), url, test.newTab,
				func(_ context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
					calls++
					if calls == 1 {
						if name != "/usr/bin/osascript" || timeout != 45*time.Second || len(args) != 7 || args[0] != "-l" || args[1] != "JavaScript" || args[2] != "-e" || args[3] != prBrowserScript || args[4] != "--" || args[5] != url {
							t.Fatalf("unexpected browser command %q / %v", name, args[:min(len(args), 3)])
						}
						if (args[6] == "true") != test.newTab {
							t.Fatal("wrong navigation mode")
						}
						return []byte(test.result), test.runError
					}
					if calls != 2 || name != "/usr/bin/open" || !reflect.DeepEqual(args, []string{url}) || timeout != 10*time.Second {
						t.Fatalf("unexpected fallback command %q / %v", name, args)
					}
					return nil, test.openError
				})
			if (err != nil) != test.wantError || (calls == 2) != test.wantFallback || !strings.Contains(message, test.wantMessage) {
				t.Fatalf("message=%q err=%v calls=%d", message, err, calls)
			}
		})
	}
}

func TestBrowserNavigationPreservesHelperFailureDetails(t *testing.T) {
	_, err := openPullRequestBrowserWith(context.Background(), "https://github.com/team/project/pull/25", false,
		func(context.Context, time.Duration, string, ...string) ([]byte, error) {
			return nil, &commandFailure{err: errors.New("exit status 1"), stderr: "execution error: null tab list (-2700)\n"}
		})
	if err == nil || !strings.Contains(err.Error(), "null tab list (-2700)") {
		t.Fatalf("missing diagnostic: %v", err)
	}
}
