package server

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/code-slammer/slammer-core/internal/agentapi"
)

func TestHealth(t *testing.T) {
	srv, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestTasksToJobsRejectsHostPaths(t *testing.T) {
	srv, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	_, err = srv.tasksToJobs([]Task{{Type: agentapi.JobWriteFile, Path: "/tmp/evil", ContentsBase64: base64.StdEncoding.EncodeToString([]byte("x"))}}, "/workspace")
	if err == nil {
		t.Fatal("expected host path rejection")
	}
}

func TestTasksToJobsAllowsMultipleWorkspaceTasks(t *testing.T) {
	srv, err := New(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := srv.tasksToJobs([]Task{
		{Type: agentapi.JobWriteFile, Path: "/workspace/main.py", ContentsBase64: base64.StdEncoding.EncodeToString([]byte("print('hi')\n"))},
		{Type: agentapi.JobExec, Argv: []string{"python3", "main.py"}, WorkingDir: "/workspace"},
	}, "/workspace")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 3 {
		t.Fatalf("len(jobs) = %d, want 3", len(jobs))
	}
}
