package utils

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Largeur de la colonne du message. Les attributs commencent après, ce qui les
// aligne d'une ligne à l'autre et rend un flux de logs balayable à l'œil.
// Un message plus long déborde simplement : mieux vaut casser l'alignement que
// tronquer une information.
const prettyMessageWidth = 46

// prettyHandler écrit des logs pensés pour être lus par un humain dans un
// terminal.
//
// slog.TextHandler impose son ordre — time, level, source, msg, attributs — ce
// qui noie le message au milieu des métadonnées, et aucun ReplaceAttr ne permet
// de réordonner. D'où ce handler : l'heure est courte, le message vient tôt et
// occupe une colonne fixe, la source passe en fin de ligne.
//
// Pour de la consommation machine, utiliser LOG_FORMAT=json.
type prettyHandler struct {
	mutex     *sync.Mutex
	writer    io.Writer
	level     slog.Leveler
	addSource bool

	// Attributs déjà formatés, hérités des appels à WithAttrs.
	preformatted string
	groups       []string
}

func NewPrettyHandler(w io.Writer, opts *slog.HandlerOptions) *prettyHandler {
	h := &prettyHandler{
		mutex:  &sync.Mutex{},
		writer: w,
		level:  slog.LevelInfo,
	}
	if opts != nil {
		if opts.Level != nil {
			h.level = opts.Level
		}
		h.addSource = opts.AddSource
	}
	return h
}

func (this *prettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= this.level.Level()
}

func (this *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return this
	}

	clone := *this
	var sb strings.Builder
	sb.WriteString(this.preformatted)
	for _, attr := range attrs {
		appendPrettyAttr(&sb, this.groups, attr)
	}
	clone.preformatted = sb.String()
	return &clone
}

func (this *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return this
	}

	clone := *this
	// Clip pour que deux branches issues du même handler ne partagent pas le
	// tableau sous-jacent.
	clone.groups = append(slices.Clip(this.groups), name)
	return &clone
}

func (this *prettyHandler) Handle(_ context.Context, record slog.Record) error {
	var builder strings.Builder

	if !record.Time.IsZero() {
		builder.WriteString(record.Time.Format("15:04:05.000"))
		builder.WriteByte(' ')
	}

	builder.WriteString(prettyLevel(record.Level))
	builder.WriteByte(' ')

	builder.WriteString(record.Message)
	// Padding en runes : les messages peuvent contenir des accents.
	for i := utf8.RuneCountInString(record.Message); i < prettyMessageWidth; i++ {
		builder.WriteByte(' ')
	}

	builder.WriteString(this.preformatted)
	record.Attrs(func(attr slog.Attr) bool {
		appendPrettyAttr(&builder, this.groups, attr)
		return true
	})

	// La source ferme la ligne : utile pour retrouver le code, mais ce n'est
	// pas ce qu'on lit en premier.
	if this.addSource && record.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{record.PC}).Next()
		if frame.File != "" {
			builder.WriteByte(' ')
			builder.WriteString(shortSource(frame.File, frame.Line))
		}
	}

	builder.WriteByte('\n')

	this.mutex.Lock()
	defer this.mutex.Unlock()
	_, err := io.WriteString(this.writer, builder.String())
	return err
}

// appendPrettyAttr écrit un attribut sous la forme ` clé=valeur`, en aplatissant
// les groupes avec des points.
func appendPrettyAttr(builder *strings.Builder, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()

	// slog demande d'ignorer les attributs vides.
	if attr.Equal(slog.Attr{}) {
		return
	}

	if attr.Value.Kind() == slog.KindGroup {
		inner := attr.Value.Group()
		if len(inner) == 0 {
			return
		}
		// Un groupe sans nom fusionne ses attributs dans le parent.
		next := groups
		if attr.Key != "" {
			next = append(slices.Clip(groups), attr.Key)
		}
		for _, sub := range inner {
			appendPrettyAttr(builder, next, sub)
		}
		return
	}

	builder.WriteByte(' ')
	for _, group := range groups {
		builder.WriteString(group)
		builder.WriteByte('.')
	}
	builder.WriteString(attr.Key)
	builder.WriteByte('=')
	builder.WriteString(prettyValue(attr.Value))
}

// prettyValue rend une valeur, en la mettant entre guillemets uniquement si elle
// contient de quoi casser la lecture d'une paire clé=valeur.
func prettyValue(value slog.Value) string {
	str := value.String()
	if str == "" {
		return `""`
	}
	if strings.ContainsAny(str, " \t\n\"=") {
		return strconv.Quote(str)
	}
	return str
}

// prettyLevel rend le niveau sur une largeur fixe, pour que les messages
// s'alignent quel que soit le niveau.
func prettyLevel(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return "DEBUG"
	case level < slog.LevelWarn:
		return "INFO "
	case level < slog.LevelError:
		return "WARN "
	case level < slog.LevelError+4:
		return "ERROR"
	default:
		return fmt.Sprintf("%-5s", level.String())
	}
}

// shortSource garde le dossier et le fichier : `scaler/scaler.go:98` situe le
// code sans étaler le chemin de build du conteneur.
func shortSource(file string, line int) string {
	dir, name := filepath.Split(file)
	return filepath.Join(filepath.Base(filepath.Clean(dir)), name) + ":" + strconv.Itoa(line)
}
