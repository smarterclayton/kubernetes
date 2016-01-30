/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package framer implements simple frame decoding techniques for an io.ReadCloser
package framer

import (
	"encoding/binary"
	"encoding/json"
	"io"
)

type lengthDelimitedFrameWriter struct {
	w io.Writer
}

func NewLengthDelimitedFrameWriter(w io.Writer) io.Writer {
	return &lengthDelimitedFrameWriter{w: w}
}

func (w *lengthDelimitedFrameWriter) Write(data []byte) (int, error) {
	header := [4]byte{}
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	n, err := w.w.Write(header[:])
	if err != nil {
		return 0, err
	}
	if n != len(header) {
		return 0, io.ErrShortWrite
	}
	return w.w.Write(data)
}

type lengthDelimitedFrameReader struct {
	r io.Reader
}

// NewLengthDelimitedFrameReader returns an io.Reader that will decode length-prefixed
// frames off of a stream.
//
// The protocol is:
//
//   stream: message ...
//   message: prefix body
//   prefix: 4 byte uint32 in BigEndian order, denotes length of body
//   body: bytes (0..prefix)
//
// TODO: the buffer sent to Read() must be at least as large as any frame that is read,
//   fix this so that clients can retry with a larger buffer
func NewLengthDelimitedFrameReader(r io.Reader) io.Reader {
	return &lengthDelimitedFrameReader{r: r}
}

func (r *lengthDelimitedFrameReader) Read(data []byte) (int, error) {
	header := [4]byte{}
	n, err := io.ReadAtLeast(r.r, header[:4], 4)
	if err != nil {
		return 0, err
	}
	if n != 4 {
		return 0, io.ErrUnexpectedEOF
	}
	frameLength := int(binary.BigEndian.Uint32(header[:]))
	if frameLength > len(data) {
		return 0, io.ErrShortBuffer
	}

	n, err = io.ReadAtLeast(r.r, data, int(frameLength))
	if err != nil {
		return n, err
	}
	if n != frameLength {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

type jsonFrameReader struct {
	decoder *json.Decoder
}

// NewJSONFramedReader returns an io.Reader that will decode individual JSON objects off
// of a wire.
//
// The boundaries between each frame are valid JSON objects. A JSON parsing error will terminate
// the read.
func NewJSONFramedReader(r io.Reader) io.Reader {
	return &jsonFrameReader{
		decoder: json.NewDecoder(r),
	}
}

// ReadFrame decodes the next JSON object in the stream, or returns an error. The returned
// byte slice will be modified the next time ReadFrame is invoked and should not be altered.
func (r *jsonFrameReader) Read(data []byte) (int, error) {
	m := json.RawMessage(data[:0])
	if err := r.decoder.Decode(&m); err != nil {
		return 0, err
	}
	return len(data), nil
}
