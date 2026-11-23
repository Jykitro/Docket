package service

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func testIcon(t *testing.T, side int, pixel uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	img.Set(0, 0, color.NRGBA{R: pixel, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCustomProjectIconLifecycle(t *testing.T) {
	s := newSvc(t)
	raw := testIcon(t, 128, 42)
	p, err := s.CreateProject(ctx, ActorDenis, ProjectInput{Name: "Logo", IconPNG: raw})
	if err != nil || p.IconURL == "" || p.IconPNG != nil {
		t.Fatalf("create: %+v %v", p, err)
	}
	initial := p.IconURL
	replacement := testIcon(t, 128, 120)
	p, err = s.UpdateProject(ctx, ActorDenis, p.ID, ProjectPatch{IconPNG: &replacement})
	if err != nil || p.IconURL == initial {
		t.Fatalf("replace: %+v %v", p, err)
	}
	for _, invalid := range [][]byte{testIcon(t, 64, 0), raw[:len(raw)/2], make([]byte, 128*1024+1)} {
		changed := "invalid change"
		if _, err := s.UpdateProject(ctx, ActorDenis, p.ID, ProjectPatch{Name: &changed, IconPNG: &invalid}); err == nil {
			t.Fatal("accepted invalid PNG")
		}
		if _, err := s.CreateProject(ctx, ActorDenis, ProjectInput{Name: changed, IconPNG: invalid}); err == nil {
			t.Fatal("created project with invalid PNG")
		}
	}
	list, err := s.ListProjects(ctx, false)
	if err != nil || len(list) != 1 || list[0].Name != p.Name || list[0].IconPNG != nil || list[0].IconURL != p.IconURL {
		t.Fatalf("list: %+v %v", list, err)
	}
	if _, err := s.DeleteProject(ctx, ActorDenis, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreProject(ctx, ActorDenis, p.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := s.ProjectIcon(ctx, p.ID)
	if err != nil || !bytes.Equal(saved, replacement) {
		t.Fatalf("restored pixels: %v", err)
	}
	for _, scope := range []int64{0, p.ID} {
		e, err := s.Export(ctx, scope)
		if err != nil || len(e.Projects) != 1 || !bytes.Equal(saved, e.Projects[0].IconPNG) {
			t.Fatalf("export %d: %v", scope, err)
		}
		audit, err := json.Marshal(e.Activity)
		if err != nil || bytes.Contains(audit, []byte("iVBOR")) {
			t.Fatal("unexpected image data in history")
		}
	}
	glyph := "stack"
	p, err = s.UpdateProject(ctx, ActorDenis, p.ID, ProjectPatch{Icon: &glyph})
	if err != nil || p.IconURL != "" || p.Icon != glyph {
		t.Fatalf("reset: %+v %v", p, err)
	}
	if _, err := s.ProjectIcon(ctx, p.ID); err == nil {
		t.Fatal("old icon still exists")
	}
}
