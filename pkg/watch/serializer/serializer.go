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

// Package serializer implements encoder and decoder for streams
// of watch events over io.Writer/Readers
package serializer

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/util/framer"
	"k8s.io/kubernetes/pkg/watch"
)

type Decoder struct {
	reader  framer.FrameReadCloser
	decoder runtime.Decoder
}

func NewDecoder(r framer.FrameReader, d runtime.Decoder) *Encoder {
	return &Encoder{
		reader:  r,
		decoder: d,
	}
}

func (d *Decoder) Decode(defaults *unversioned.GroupVersionKind, into runtime.Object) (runtime.Object, *unversioned.GroupVersionKind, error) {
	data, err := d.reader.ReadFrame()
	if err != nil {
		return nil, err
	}
	return d.decoder.Decode(data, defaults, into)
}

type Encoder struct {
	writer  framer.FrameWriter
	encoder runtime.Encoder
	buf     *bytes.Buffer
}

func NewEncoder(w framer.FrameWriter, e runtime.Encoder) *Encoder {
	return &Encoder{
		writer:  w,
		encoder: e,
		buf:     &bytes.Buffer{},
	}
}

func (e *Encoder) Encode(obj runtime.Object, overrides ...unversioned.GroupVersion) error {
	if err := e.encoder.EncodeToStream(obj, e.buf, overrides...); err != nil {
		return err
	}
	_, err := e.writer.Write(e.buf.Bytes())
	e.buf.Reset()
	return err
}
