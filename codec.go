package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
)

type Encoder interface {
	Encode(v interface{}) ([]byte, error)
}

type Decoder interface {
	Decode(data []byte, v interface{}) error
}

type JSONCodec struct{}

func (c JSONCodec) Encode(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (c JSONCodec) Decode(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

type GobCodec struct{}

func (c GobCodec) Encode(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(v)
	return buf.Bytes(), err
}

func (c GobCodec) Decode(data []byte, v interface{}) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(v)
}

func MustEncode(enc Encoder, v interface{}) []byte {
	data, err := enc.Encode(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TryDecode(dec Decoder, data []byte, v interface{}) bool {
	return dec.Decode(data, v) == nil
}
