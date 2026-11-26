package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image/png"

	"plandillo/internal/store"
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
	data, err := NormalizeIcon(data)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	return tx.SetProjectIcon(id, data, hex.EncodeToString(sum[:16]))
}

// ValidateIconName accepts the built-in glyphs offered in both pickers.
func ValidateIconName(icon string) error { return oneOf("icon", icon, projectIcons) }

// NormalizeIcon validates and re-encodes uploaded profile and workspace images.
func NormalizeIcon(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) > 128*1024 {
		return nil, invalid("icon_png must be at most 128 KiB")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 128 || cfg.Height != 128 {
		return nil, invalid("icon_png must be a 128 by 128 PNG")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, invalid("icon_png is not a valid PNG")
	}
	// Store only decoded pixels. Discard metadata and any bytes following IEND.
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
