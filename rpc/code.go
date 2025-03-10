package rpc

type CodeOutput struct {
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

type Code struct {
	Code string `json:"code"`
	Type string `json:"type"`
}
