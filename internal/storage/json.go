package storage

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
)

func MarshalJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func MarshalGzipJSON(v any) ([]byte, error) {
	raw, err := MarshalJSON(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func UnmarshalMaybeGzip(data []byte, v any) error {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return err
		}
		defer zr.Close()
		data, err = io.ReadAll(zr)
		if err != nil {
			return err
		}
	}
	return json.Unmarshal(data, v)
}
