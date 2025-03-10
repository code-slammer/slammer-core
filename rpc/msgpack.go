package rpc

import (
	"io"

	"github.com/vmihailenco/msgpack/v5"
)

type MsgPackSerializer struct{}

var _ Serializer = (*MsgPackSerializer)(nil)

func (m *MsgPackSerializer) Serialize(w io.Writer, o any) error {
	enc := msgpack.NewEncoder(w)
	enc.SetCustomStructTag("json")
	err := enc.Encode(o)
	if err != nil {
		return err
	}
	return nil
}

func (m *MsgPackSerializer) Deserialize(data io.Reader, o any) error {
	dec := msgpack.NewDecoder(data)
	dec.SetCustomStructTag("json")
	err := dec.Decode(o)
	if err != nil {
		return err
	}
	return nil
}
