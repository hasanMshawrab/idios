package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpMockListen is where the harness's fixture daemon listens. It is not
// the default port: a daemon the user is running must never be reached by
// a test, and must never be shadowed by one.
const mcpMockListen = "127.0.0.1:7772"

// The MCP server is a client of the daemon and nothing else, so every tool
// has to answer with what the API answered: the fixture bytes, whole.
func TestMCPToolsAnswerWithTheDaemonsBytes(t *testing.T) {
	bin := buildIdios(t)
	startMock(t, bin)
	session := connectMCP(t, bin, mcpMockListen)

	cases := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{name: "list_incidents is the incidents response", tool: "list_incidents", args: map[string]any{},
			want: fixture(t, "incidents")},
		{name: "list_incidents scopes to a named cluster", tool: "list_incidents", args: map[string]any{"cluster": "prod", "state": "open"},
			want: fixture(t, "incidents")},
		{name: "get_incident carries the timeline with the detail", tool: "get_incident", args: map[string]any{"id": 412},
			want: `{"incident":` + fixture(t, "incident_detail") + `,"timeline":` + fixture(t, "timeline") + `}`},
		{name: "get_pod carries the whole event stream and the history", tool: "get_pod", args: map[string]any{"uid": "pod-crash"},
			want: `{"pod":` + fixture(t, "pod_detail") + `,"events":` + fixture(t, "events") + `,"history":` + fixture(t, "history") + `}`},
		{name: "get_workload is the workload detail", tool: "get_workload",
			args: map[string]any{"cluster": "prod", "namespace": "shop", "kind": "deployment", "name": "checkout-api"},
			want: fixture(t, "workload_detail")},
		{name: "read_log names the file it cut and the cut it made", tool: "read_log", args: map[string]any{"artifact_id": 81},
			want: "artifact 81 kind=log_previous container=api restart=6 captured_at=2026-08-27T14:38:05.000000Z" +
				" bytes=14 truncated_by_capture=true\nshowing: the whole file, 1 line\n\nmock log line\n"},
		{name: "read_log greps the captured lines", tool: "read_log", args: map[string]any{"artifact_id": 81, "grep": "^mock"},
			want: "artifact 81 kind=log_previous container=api restart=6 captured_at=2026-08-27T14:38:05.000000Z" +
				" bytes=14 truncated_by_capture=true\nshowing: 1 line matching \"^mock\"\n\nmock log line\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := callTool(t, session, c.tool, c.args); got != c.want {
				t.Errorf("%s =\n%s\nwant\n%s", c.tool, got, c.want)
			}
		})
	}

	t.Run("get_status carries the cluster names the other tools take", func(t *testing.T) {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(fixture(t, "status")), &raw); err != nil {
			t.Fatal(err)
		}
		// Nothing records behind the fixture server, and a false proto3
		// scalar is not on the wire at all.
		delete(raw, "daemonRunning")
		status, err := json.Marshal(raw)
		if err != nil {
			t.Fatal(err)
		}
		want := `{"status":` + string(status) + `,"clusters":` + fixture(t, "clusters") + `}`
		if got := callTool(t, session, "get_status", map[string]any{}); got != want {
			t.Errorf("get_status =\n%s\nwant\n%s", got, want)
		}
	})

	t.Run("read_pod_json refuses an artifact that is not one", func(t *testing.T) {
		want := "artifact 81 is a log_previous, not a captured pod.json; read it with read_log"
		if got := callToolError(t, session, "read_pod_json", map[string]any{"artifact_id": 81}); got != want {
			t.Errorf("read_pod_json error = %q, want %q", got, want)
		}
	})

	t.Run("an unknown cluster name lists the ones that are watched", func(t *testing.T) {
		want := `no cluster named "nowhere": this daemon watches [prod staging]`
		if got := callToolError(t, session, "list_incidents", map[string]any{"cluster": "nowhere"}); got != want {
			t.Errorf("list_incidents error = %q, want %q", got, want)
		}
	})
}

// A tool call that reaches nothing must say where it looked: a raw dial
// failure tells an agent to retry, which is never the answer.
func TestMCPToolsNameTheAddressWhenNothingAnswers(t *testing.T) {
	bin := buildIdios(t)
	addr := freeAddr(t)
	session := connectMCP(t, bin, addr)
	want := "no idios daemon answered at " + addr +
		": start one with 'idios run', or point this server elsewhere with 'idios mcp -daemon host:port'"

	for _, tool := range []struct {
		name string
		args map[string]any
	}{
		{"list_incidents", map[string]any{}},
		{"get_incident", map[string]any{"id": 1}},
		{"get_pod", map[string]any{"uid": "pod-crash"}},
		{"read_log", map[string]any{"artifact_id": 1}},
		{"read_pod_json", map[string]any{"artifact_id": 1}},
		{"get_workload", map[string]any{"cluster": "prod", "namespace": "shop", "kind": "deployment", "name": "checkout-api"}},
		{"get_status", map[string]any{}},
	} {
		t.Run(tool.name, func(t *testing.T) {
			if got := callToolError(t, session, tool.name, tool.args); got != want {
				t.Errorf("%s error = %q, want %q", tool.name, got, want)
			}
		})
	}
}

// fixture is the wire bytes of one committed response, compacted the way
// every answer that crosses the protocol is.
func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "api", "testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// buildIdios builds the command under test, so the harness drives the same
// binary a user configures their agent with.
func buildIdios(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "idios")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// startMock runs the fixture server on the harness's port until the test
// ends, and returns once it answers.
func startMock(t *testing.T, bin string) {
	t.Helper()
	cmd := exec.Command(bin, "-data-dir", t.TempDir(), "mock", "-listen", mcpMockListen)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get("http://" + mcpMockListen + "/v1/status")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("idios mock did not answer on %s: %v", mcpMockListen, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// connectMCP starts the server as the agent does, over stdio.
func connectMCP(t *testing.T, bin, daemon string) *sdk.ClientSession {
	t.Helper()
	cmd := exec.Command(bin, "-data-dir", t.TempDir(), "mcp", "-daemon", daemon)
	cmd.Stderr = os.Stderr
	client := sdk.NewClient(&sdk.Implementation{Name: "idios-test", Version: "0"}, nil)
	session, err := client.Connect(t.Context(), &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// callTool calls one tool and returns its text, failing the test when the
// tool reported an error.
func callTool(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	got, isError := call(t, session, name, args)
	if isError {
		t.Fatalf("%s failed: %s", name, got)
	}
	return got
}

// callToolError calls one tool and returns the text of the error it
// reported, failing the test when it reported none.
func callToolError(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	got, isError := call(t, session, name, args)
	if !isError {
		t.Fatalf("%s succeeded with %s, want an error", name, got)
	}
	return got
}

func call(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	for _, c := range res.Content {
		text, ok := c.(*sdk.TextContent)
		if !ok {
			t.Fatalf("%s returned %T, want text", name, c)
		}
		b.WriteString(text.Text)
	}
	return b.String(), res.IsError
}

// freeAddr is a loopback address nothing is listening on.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}
