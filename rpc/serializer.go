package rpc

import "io"

var Default_Serializer = &MsgPackSerializer{}

type Serializer interface {
	Serialize(w io.Writer, o any) error
	Deserialize(r io.Reader, o any) error
}
