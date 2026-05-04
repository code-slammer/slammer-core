package agentapi

const (
	Version = 1

	JobMkdir     = "mkdir"
	JobWriteFile = "write_file"
	JobExec      = "exec"
)

type BatchRequest struct {
	Version   int         `json:"version"`
	Workspace string      `json:"workspace"`
	Defaults  JobDefaults `json:"defaults"`
	Jobs      []Job       `json:"jobs"`
	Shutdown  bool        `json:"shutdown"`
}

type JobDefaults struct {
	UID            *int     `json:"uid,omitempty"`
	GID            *int     `json:"gid,omitempty"`
	Env            []string `json:"env,omitempty"`
	TimeoutMillis  int64    `json:"timeout_ms,omitempty"`
	MaxOutputBytes int64    `json:"max_output_bytes,omitempty"`
}

type Job struct {
	Type string `json:"type"`

	Path           string `json:"path,omitempty"`
	Mode           uint32 `json:"mode,omitempty"`
	ContentsBase64 string `json:"contents_b64,omitempty"`
	CreateParents  bool   `json:"create_parents,omitempty"`

	Argv           []string `json:"argv,omitempty"`
	Env            []string `json:"env,omitempty"`
	WorkingDir     string   `json:"working_dir,omitempty"`
	UID            *int     `json:"uid,omitempty"`
	GID            *int     `json:"gid,omitempty"`
	TimeoutMillis  int64    `json:"timeout_ms,omitempty"`
	MaxOutputBytes int64    `json:"max_output_bytes,omitempty"`
}

type BatchResponse struct {
	Results []JobResult `json:"results"`
}

type JobResult struct {
	Type  string `json:"type"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	ExitCode int  `json:"exit_code,omitempty"`
	TimedOut bool `json:"timed_out,omitempty"`

	StdoutBase64 string `json:"stdout_b64,omitempty"`
	StderrBase64 string `json:"stderr_b64,omitempty"`
}
