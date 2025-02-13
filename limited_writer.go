package main

import "io"

type LimitedWriter struct {
	W io.Writer // underlying writer
	N int       // max bytes remaining
}

// NewLimitedWriter returns a new LimitedWriter with underlying writer w and n bytes limit.
func NewLimitedWriter(w io.Writer, n int) *LimitedWriter {
	return &LimitedWriter{W: w, N: n}
}

func (l *LimitedWriter) Write(p []byte) (n int, err error) {
	if l.N <= 0 { // if no bytes remaining
		return 0, io.EOF
	}
	if len(p) > l.N { // if p is larger than the remaining limit
		n, err = l.W.Write(p[:l.N]) // write only the first N bytes
		l.N = 0                     // set remaining bytes to 0
		return n, io.EOF            // return EOF error
	}
	n, err = l.W.Write(p) // write the whole p
	l.N -= len(p)         // decrement remaining bytes
	return
}
