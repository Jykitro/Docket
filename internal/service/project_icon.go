package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image/png"

	"docket/internal/store"
)

func (s *Service) ProjectIcon(ctx context.Context, id int64) (data []byte, err error) {
	err = s.read(ctx, func(tx *store.Tx) error {
		data, err = tx.ProjectIcon(id)
		return err
	})
	return data, err
}

func setProjectIcon(tx *store.Tx, id int64, data []byte) error {
	if len(data) == 0 {
		return tx.SetProjectIcon(id, nil, "")
	}
	if len(data) > 128*1024 {
		return invalid("icon_png must be at most 128 KiB")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 128 || cfg.Height != 128 {
		return invalid("icon_png must be a 128 by 128 PNG")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return invalid("icon_png is not a valid PNG")
	}
	// Store only decoded pixels. Discard metadata and any bytes following IEND.
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return err
	}
	sum := sha256.Sum256(out.Bytes())
	return tx.SetProjectIcon(id, out.Bytes(), hex.EncodeToString(sum[:16]))
}
