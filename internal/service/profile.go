package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"docket/internal/store"
)

// The profile is just a display name; an empty one means first run and the UI starts onboarding.
// The stored actor "denis" refers to the profile owner. The UI shows the profile name.
const settingProfileName = "profile_name"

const maxProfileName = 40

// SetStorageLimit opts a hosted PostgreSQL workspace into a byte quota before serving it.
func (s *Service) SetStorageLimit(bytes int64) { s.storageLimit = bytes }

type StorageUsage struct {
	Used  int64 `json:"used"`
	Limit int64 `json:"limit"`
}
type Profile struct {
	Storage *StorageUsage `json:"storage,omitempty"`
	Name    string        `json:"name"`
}

func (s *Service) GetProfile(ctx context.Context) (p Profile, err error) {
	err = s.read(ctx, func(tx *store.Tx) error {
		p.Name, _, err = tx.Setting(settingProfileName)
		if err != nil || s.storageLimit == 0 {
			return err
		}
		used, err := tx.WorkspaceBytes()
		p.Storage = &StorageUsage{Used: used, Limit: s.storageLimit}
		return err
	})
	return p, err
}

func (s *Service) SetProfile(ctx context.Context, name string) (Profile, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > maxProfileName {
		return Profile{}, invalid("name must be 1–%d characters", maxProfileName)
	}
	err := s.write(ctx, ActorDenis, func(o *op) error {
		o.emit("settings_changed", 0, 0)
		return o.tx.SetSetting(settingProfileName, name)
	})
	return Profile{Name: name}, err
}
