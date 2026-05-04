package agentserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/code-slammer/slammer-core/internal/agentapi"
)

func TestJobsWriteThenExec(t *testing.T) {
	workspace := t.TempDir()
	server := New(Config{Workspace: workspace, DefaultUID: os.Getuid(), DefaultGID: os.Getgid()})

	req := agentapi.BatchRequest{
		Version:   agentapi.Version,
		Workspace: workspace,
		Jobs: []agentapi.Job{
			{
				Type:           agentapi.JobWriteFile,
				Path:           filepath.Join(workspace, "script.sh"),
				Mode:           0o700,
				ContentsBase64: base64.StdEncoding.EncodeToString([]byte("#!/bin/sh\nprintf hello")),
			},
			{
				Type:       agentapi.JobExec,
				Argv:       []string{filepath.Join(workspace, "script.sh")},
				WorkingDir: workspace,
			},
		},
	}

	resp := postJobs(t, server, req)
	if len(resp.Results) != 2 {
		t.Fatalf("got %d results", len(resp.Results))
	}
	if !resp.Results[0].OK || !resp.Results[1].OK {
		t.Fatalf("unexpected failure: %+v", resp.Results)
	}
	stdout, err := base64.StdEncoding.DecodeString(resp.Results[1].StdoutBase64)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdout) != "hello" {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestJobsRejectsPathEscape(t *testing.T) {
	workspace := t.TempDir()
	server := New(Config{Workspace: workspace, DefaultUID: os.Getuid(), DefaultGID: os.Getgid()})

	req := agentapi.BatchRequest{
		Version:   agentapi.Version,
		Workspace: workspace,
		Jobs: []agentapi.Job{{
			Type:           agentapi.JobWriteFile,
			Path:           filepath.Join(workspace, "..", "escape"),
			ContentsBase64: base64.StdEncoding.EncodeToString([]byte("bad")),
		}},
	}

	body, _ := json.Marshal(req)
	recorder := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(body))
	server.Handler().ServeHTTP(recorder, httpReq)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestJobsRejectsSymlinkParent(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "link")); err != nil {
		t.Fatal(err)
	}
	server := New(Config{Workspace: workspace, DefaultUID: os.Getuid(), DefaultGID: os.Getgid()})

	req := agentapi.BatchRequest{
		Version:   agentapi.Version,
		Workspace: workspace,
		Jobs: []agentapi.Job{{
			Type:           agentapi.JobWriteFile,
			Path:           filepath.Join(workspace, "link", "file"),
			ContentsBase64: base64.StdEncoding.EncodeToString([]byte("bad")),
		}},
	}

	body, _ := json.Marshal(req)
	recorder := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(body))
	server.Handler().ServeHTTP(recorder, httpReq)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestJobsIsOneShot(t *testing.T) {
	workspace := t.TempDir()
	server := New(Config{Workspace: workspace, DefaultUID: os.Getuid(), DefaultGID: os.Getgid()})
	req := agentapi.BatchRequest{Version: agentapi.Version, Workspace: workspace, Jobs: []agentapi.Job{{Type: agentapi.JobMkdir, Path: filepath.Join(workspace, "x")}}}

	_ = postJobs(t, server, req)
	body, _ := json.Marshal(req)
	recorder := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(body))
	server.Handler().ServeHTTP(recorder, httpReq)
	if recorder.Code != http.StatusGone {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func postJobs(t *testing.T, server *Server, req agentapi.BatchRequest) agentapi.BatchResponse {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewReader(body))
	server.Handler().ServeHTTP(recorder, httpReq)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var resp agentapi.BatchResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}
