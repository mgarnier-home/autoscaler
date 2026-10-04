package utils

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(NewPrettyHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// Le message doit venir avant les attributs et occuper une colonne fixe : c'est
// tout l'intérêt du handler par rapport à slog.TextHandler.
func TestPrettyHandlerPutsMessageFirst(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf).Info("Removing idle runner", slog.String("name", "runner-1"))

	line := buf.String()
	msgAt := strings.Index(line, "Removing idle runner")
	attrAt := strings.Index(line, "name=runner-1")

	if msgAt < 0 || attrAt < 0 {
		t.Fatalf("message ou attribut absent: %q", line)
	}
	if msgAt > attrAt {
		t.Fatalf("le message doit précéder les attributs: %q", line)
	}
	if attrAt < prettyMessageWidth {
		t.Fatalf("les attributs devraient commencer après la colonne %d: %q", prettyMessageWidth, line)
	}
}

// Deux messages de longueurs différentes doivent aligner leurs attributs.
func TestPrettyHandlerAlignsAttributes(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)
	l.Info("Court", slog.Int("n", 1))
	l.Info("Un message nettement plus long", slog.Int("n", 2))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("attendu 2 lignes, obtenu %d", len(lines))
	}
	if strings.Index(lines[0], "n=1") != strings.Index(lines[1], "n=2") {
		t.Fatalf("attributs non alignés:\n%s\n%s", lines[0], lines[1])
	}
}

func TestPrettyHandlerGroupsAndAttrs(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf).
		With(slog.String("scaleSet", "local")).
		WithGroup("listener").
		Info("Getting next message", slog.Int("lastMessageID", 42))

	line := buf.String()
	for _, want := range []string{"scaleSet=local", "listener.lastMessageID=42"} {
		if !strings.Contains(line, want) {
			t.Fatalf("attendu %q dans %q", want, line)
		}
	}
}

// WithGroup ne doit pas contaminer un handler frère : les deux branches
// partageraient sinon le tableau de groupes sous-jacent.
func TestPrettyHandlerGroupIsolation(t *testing.T) {
	var buf bytes.Buffer
	base := newTestLogger(&buf)

	base.WithGroup("a").Info("x", slog.Int("k", 1))
	base.WithGroup("b").Info("y", slog.Int("k", 2))

	line := buf.String()
	if !strings.Contains(line, "a.k=1") || !strings.Contains(line, "b.k=2") {
		t.Fatalf("groupes mal isolés: %q", line)
	}
}

// Une valeur contenant un espace ou un `=` doit être mise entre guillemets,
// sinon la ligne n'est plus découpable en paires clé=valeur.
func TestPrettyHandlerQuotesAmbiguousValues(t *testing.T) {
	var buf bytes.Buffer
	newTestLogger(&buf).Error("boom", slog.String("error", "no such container"))

	if !strings.Contains(buf.String(), `error="no such container"`) {
		t.Fatalf("valeur non échappée: %q", buf.String())
	}
}

func TestPrettyHandlerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	h := NewPrettyHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})

	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("INFO ne devrait pas passer avec un niveau WARN")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Fatal("ERROR devrait passer avec un niveau WARN")
	}
}

func TestShortSource(t *testing.T) {
	got := shortSource("/build/scaler/scaler.go", 98)
	if got != "scaler/scaler.go:98" {
		t.Fatalf("shortSource = %q, attendu scaler/scaler.go:98", got)
	}
}
