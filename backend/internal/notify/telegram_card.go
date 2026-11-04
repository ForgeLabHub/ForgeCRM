package notify

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"time"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// TaskCard is a projection of the CRM task, not a second task model or store.
type TaskCard struct {
	Kind     string // assignment, due_changed, overdue, completed
	Locale   string
	Title    string
	Priority string
	DueAt    *time.Time
}

// RenderTaskCard produces a deterministic PNG and an equivalent plain text
// message. Fonts are embedded in the binary; no network or AI service is used.
func RenderTaskCard(c TaskCard) ([]byte, string, error) {
	ru := strings.HasPrefix(strings.ToLower(c.Locale), "ru")
	labels := map[string][2]string{
		"assignment":  {"Task assigned", "Назначена задача"},
		"due_changed": {"Task due date changed", "Срок задачи изменён"},
		"overdue":     {"Task overdue", "Задача просрочена"},
		"completed":   {"Task completed", "Задача выполнена"},
	}
	label, ok := labels[c.Kind]
	if !ok {
		return nil, "", errors.New("unknown task card kind")
	}
	title := cleanCardTitle(c.Title)
	if title == "" {
		return nil, "", errors.New("empty task card title")
	}
	header := label[0]
	dueLabel := "Due"
	priorityLabel := "Priority"
	none := "not set"
	if ru {
		header = label[1]
		dueLabel = "Срок"
		priorityLabel = "Приоритет"
		none = "не указан"
	}
	due := none
	if c.DueAt != nil {
		due = c.DueAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	priority := cleanCardTitle(c.Priority)
	text := fmt.Sprintf("%s\n%s\n%s: %s\n%s: %s", header, title, dueLabel, due, priorityLabel, priority)
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, text, errors.New("task card font unavailable")
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 30, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, text, errors.New("task card font unavailable")
	}
	defer face.Close()
	img := image.NewRGBA(image.Rect(0, 0, 1000, 420))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 17, G: 24, B: 39, A: 255}}, image.Point{}, draw.Src)
	accent := color.RGBA{R: 85, G: 166, B: 255, A: 255}
	if c.Kind == "overdue" {
		accent = color.RGBA{R: 255, G: 108, B: 106, A: 255}
	}
	if c.Kind == "completed" {
		accent = color.RGBA{R: 91, G: 205, B: 152, A: 255}
	}
	draw.Draw(img, image.Rect(0, 0, 12, 420), &image.Uniform{C: accent}, image.Point{}, draw.Src)
	d := &font.Drawer{Dst: img, Src: image.NewUniform(color.White), Face: face}
	write := func(x, y int, s string) { d.Dot = fixed.P(x, y); d.DrawString(s) }
	write(40, 65, header)
	// Fit bounded CRM text to two lines, truncating rather than overflowing.
	words := strings.Fields(title)
	lines := []string{""}
	for _, word := range words {
		cur := strings.TrimSpace(lines[len(lines)-1] + " " + word)
		if d.MeasureString(cur).Ceil() <= 910 {
			lines[len(lines)-1] = cur
			continue
		}
		if len(lines) == 2 {
			lines[1] += "…"
			break
		}
		lines = append(lines, "")
		for _, r := range word {
			next := lines[1] + string(r)
			if d.MeasureString(next).Ceil() > 885 {
				lines[1] += "…"
				break
			}
			lines[1] = next
		}
	}
	for i, line := range lines {
		write(40, 140+i*45, line)
	}
	write(40, 285, dueLabel+": "+due)
	write(40, 345, priorityLabel+": "+priority)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, text, errors.New("task card encoding failed")
	}
	return b.Bytes(), text, nil
}

func cleanCardTitle(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n == 120 {
			b.WriteRune('…')
			break
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			r = ' '
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
