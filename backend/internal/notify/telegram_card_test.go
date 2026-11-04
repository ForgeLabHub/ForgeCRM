package notify

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

func TestTaskCardsRenderRussianAndEnglish(t *testing.T) {
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 30, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	for _, r := range "Назначена задачаПросроченаВыполнена" {
		if r != ' ' {
			if _, ok := face.GlyphAdvance(r); !ok {
				t.Fatalf("font has no glyph %q", r)
			}
		}
	}
	due := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for _, kind := range []string{"assignment", "due_changed", "overdue", "completed"} {
		for _, locale := range []string{"en", "ru"} {
			t.Run(kind+"/"+locale, func(t *testing.T) {
				data, text, err := RenderTaskCard(TaskCard{Kind: kind, Locale: locale, Title: "Реальная задача\nprivate", Priority: "normal", DueAt: &due})
				if err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(data))
				if err != nil || img.Bounds().Dx() != 1000 || img.Bounds().Dy() != 420 {
					t.Fatalf("PNG: %v", err)
				}
				if !strings.Contains(text, "Реальная задача private") || !strings.Contains(text, "2026-11-04 12:00 UTC") || strings.Contains(text, "\nprivate") {
					t.Fatalf("bad text: %q", text)
				}
			})
		}
	}
	if _, _, err := RenderTaskCard(TaskCard{Kind: "unknown", Title: "test"}); err == nil {
		t.Fatal("unknown kind accepted")
	}
}
