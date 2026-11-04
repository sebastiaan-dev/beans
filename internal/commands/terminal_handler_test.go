package commands

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/hmans/beans/internal/graph"
	"github.com/hmans/beans/internal/terminal"
)

func newTerminalTestServer(t *testing.T, termMgr *terminal.Manager, projectRoot string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterTerminalRoute(router, termMgr, nil, func(*http.Request) bool { return true }, projectRoot)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/terminal"
}

// dialTerminal opens a terminal WebSocket for sessionID and collects output until
// the server closes the connection. Returns the output and the close error.
func dialTerminal(t *testing.T, url, sessionID string) (string, *websocket.CloseError) {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "init", "sessionId": sessionID, "cols": 80, "rows": 24}); err != nil {
		t.Fatalf("write init: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var out strings.Builder
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			ce, ok := err.(*websocket.CloseError)
			if !ok {
				t.Fatalf("read: %v (output so far: %q)", err, out.String())
			}
			return out.String(), ce
		}
		out.Write(data)
	}
}

func TestTerminalWSDoesNotSpawnShellForMissingRunSession(t *testing.T) {
	termMgr := terminal.NewManager(nil)
	defer termMgr.Shutdown()
	url := newTerminalTestServer(t, termMgr, t.TempDir())

	runID := graph.CentralSessionID + graph.RunSessionSuffix
	_, ce := dialTerminal(t, url, runID)

	if ce.Code != websocket.CloseNormalClosure || ce.Text != "shell exited" {
		t.Fatalf("expected normal 'shell exited' close, got %d %q", ce.Code, ce.Text)
	}
	if termMgr.Get(runID) != nil {
		t.Fatal("no session should be created for a run session ID")
	}
}

func TestTerminalWSReplaysExitedRunSession(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	termMgr := terminal.NewManager(nil)
	defer termMgr.Shutdown()
	dir := t.TempDir()
	url := newTerminalTestServer(t, termMgr, dir)

	// A run command that finishes before any client connects.
	runID := graph.CentralSessionID + graph.RunSessionSuffix
	sess, err := termMgr.CreateWithCommand(runID, dir, 80, 24, "echo run-finished")
	if err != nil {
		t.Fatalf("CreateWithCommand: %v", err)
	}
	select {
	case <-sess.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("run command did not exit")
	}

	out, ce := dialTerminal(t, url, runID)

	if !strings.Contains(out, "run-finished") {
		t.Errorf("expected run output to be replayed, got %q", out)
	}
	if ce.Code != websocket.CloseNormalClosure || ce.Text != "shell exited" {
		t.Errorf("expected normal 'shell exited' close, got %d %q", ce.Code, ce.Text)
	}
	if got := termMgr.Get(runID); got != sess || got.Alive() {
		t.Error("the exited run session must not be replaced by a new shell")
	}
}

func TestTerminalWSCreatesShellSessionOnDemand(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	termMgr := terminal.NewManager(nil)
	defer termMgr.Shutdown()
	url := newTerminalTestServer(t, termMgr, t.TempDir())

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "init", "sessionId": graph.CentralSessionID, "cols": 80, "rows": 24}); err != nil {
		t.Fatalf("write init: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for termMgr.Get(graph.CentralSessionID) == nil {
		if time.Now().After(deadline) {
			t.Fatal("shell session was not created")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !termMgr.Get(graph.CentralSessionID).Alive() {
		t.Fatal("shell session should be alive")
	}
}
