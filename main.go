package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

type Task struct {
	Type          string   `json:"type"`
	Path          string   `json:"path,omitempty"`
	ContentsB64   string   `json:"contents_b64,omitempty"`
	Mode          int      `json:"mode,omitempty"`
	CreateParents bool     `json:"create_parents,omitempty"`
	Argv          []string `json:"argv,omitempty"`
	WorkingDir    string   `json:"working_dir,omitempty"`
}

type RunRequest struct {
	ImageRef     string `json:"image_ref"`
	SnapshotMode string `json:"snapshot_mode"`
	Tasks        []Task `json:"tasks"`
}

func main() {
	contents, err := os.ReadFile("test.py")
	must(err)
	base64Contents := base64.StdEncoding.EncodeToString(contents)
	payload := RunRequest{
		ImageRef:     "docker.io/library/python:3.14-alpine",
		SnapshotMode: "auto",
		Tasks: []Task{
			{
				Type:          "write_file",
				Path:          "/workspace/main.py",
				ContentsB64:   base64Contents,
				Mode:          420,
				CreateParents: true,
			},
			{
				Type:       "exec",
				Argv:       []string{"python3", "main.py"},
				WorkingDir: "/workspace",
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := http.Post("http://localhost:8080/v1/runs", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Status)
	fmt.Println(string(respBody))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
