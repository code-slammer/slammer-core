package rpc

import (
	"encoding/json"
	"io"
)

type JSONSerializer struct{}

func (s *JSONSerializer) Serialize(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

func (s *JSONSerializer) Deserialize(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
