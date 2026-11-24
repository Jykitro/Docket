package codeeval

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"time"
)

const (
	MaxFrame = 16 << 20
	MaxCode  = 256 << 10
)

type Message struct {
	Kind  string          `json:"kind"`
	Text  string          `json:"text,omitempty"`
	Args  json.RawMessage `json:"args,omitempty"`
	Error string          `json:"error,omitempty"`
}

func Send(w io.Writer, m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(b) > MaxFrame {
		return errors.New("message exceeds the Code Mode limit")
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(b)))
	if _, err = w.Write(size[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
func Receive(r io.Reader) (Message, error) {
	var size [4]byte
	var m Message
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return m, err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n == 0 || n > MaxFrame {
		return m, errors.New("invalid Code Mode frame size")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return m, err
	}
	err := json.Unmarshal(b, &m)
	return m, err
}

// Evaluate runs one job on stdin/stdout in a disposable child of the isolated runner.
func Evaluate(in io.Reader, out io.Writer) error {
	first, err := Receive(in)
	if err != nil {
		return err
	}
	if first.Kind != "run" || len(first.Text) > MaxCode {
		return errors.New("invalid Code Mode job")
	}
	result, err := Run(context.Background(), 30*time.Second, first.Text, func(name string, args json.RawMessage) (string, error) {
		if err := Send(out, Message{Kind: "call", Text: name, Args: args}); err != nil {
			return "", err
		}
		m, err := Receive(in)
		if err != nil {
			return "", err
		}
		if m.Kind != "reply" {
			return "", errors.New("invalid tool reply")
		}
		if m.Error != "" {
			return "", errors.New(m.Error)
		}
		return m.Text, nil
	})
	reply := Message{Kind: "done", Text: result}
	if err != nil {
		reply.Error = err.Error()
		if len(reply.Error) > 4096 {
			reply.Error = reply.Error[:4096]
		}
	}
	return Send(out, reply)
}
