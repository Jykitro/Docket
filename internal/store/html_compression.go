package store

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MaxHTMLBytes is the logical publication limit, including compressed SQLite revisions.
const MaxHTMLBytes = 2 << 20
const minHTMLCompressionBytes = 4 << 10

func compressRevision(html string) ([]byte, error) {
	if len(html) < minHTMLCompressionBytes {
		return nil, nil
	}
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err = io.WriteString(w, html); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	// Small savings do not justify paying for decompression on every read.
	if buf.Len() > len(html)-len(html)/8 {
		return nil, nil
	}
	return buf.Bytes(), nil
}

func decompressRevision(payload string, rawBytes int64) (string, error) {
	if rawBytes < 1 || rawBytes > MaxHTMLBytes {
		return "", errors.New("invalid uncompressed HTML revision size")
	}
	input := strings.NewReader(payload)
	r, err := gzip.NewReader(input)
	if err != nil {
		return "", fmt.Errorf("open compressed HTML revision: %w", err)
	}
	defer r.Close()
	r.Multistream(false)
	body, err := io.ReadAll(io.LimitReader(r, rawBytes+1))
	if err != nil {
		return "", fmt.Errorf("read compressed HTML revision: %w", err)
	}
	if int64(len(body)) != rawBytes || input.Len() != 0 {
		return "", errors.New("invalid compressed HTML revision length")
	}
	return string(body), nil
}
