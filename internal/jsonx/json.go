// Package jsonx centralizes use of encoding/json/v2 and repository JSON policy.
package jsonx

import (
	json "encoding/json/v2"
	"io"
)

func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func MarshalWrite(w io.Writer, v any) error {
	return json.MarshalWrite(w, v)
}

func UnmarshalRead(r io.Reader, v any) error {
	return json.UnmarshalRead(r, v)
}
