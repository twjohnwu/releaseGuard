package plantuml

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

type Interaction struct {
	Source string
	Target string
	Kind   string // sync_call | async_event | unknown
	Label  string
}

type Diagram struct {
	Interactions []Interaction
}

// Matches: A -> B: label    A ->> B: label    "A B" -> B
var (
	syncRe  = regexp.MustCompile(`^\s*("[^"]+"|[^\s:]+)\s+->\s+("[^"]+"|[^\s:]+)\s*:?\s*(.*)$`)
	asyncRe = regexp.MustCompile(`^\s*("[^"]+"|[^\s:]+)\s+->>\s+("[^"]+"|[^\s:]+)\s*:?\s*(.*)$`)
	aliasRe = regexp.MustCompile(`^\s*participant\s+(?:"([^"]+)"|(\S+))(?:\s+as\s+(\S+))?\s*$`)
)

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func Parse(r io.Reader) (*Diagram, error) {
	d := &Diagram{}
	aliases := map[string]string{} // alias → real participant name
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if m := aliasRe.FindStringSubmatch(line); m != nil {
			full := m[1]
			if full == "" {
				full = m[2]
			}
			short := m[3]
			if short != "" {
				aliases[short] = full
			}
			continue
		}
		// async (->>) must be checked BEFORE sync (->) since ->> contains ->
		if m := asyncRe.FindStringSubmatch(line); m != nil {
			d.Interactions = append(d.Interactions, Interaction{
				Source: resolveName(unquote(m[1]), aliases),
				Target: resolveName(unquote(m[2]), aliases),
				Kind:   "async_event",
				Label:  strings.TrimSpace(m[3]),
			})
			continue
		}
		if m := syncRe.FindStringSubmatch(line); m != nil {
			d.Interactions = append(d.Interactions, Interaction{
				Source: resolveName(unquote(m[1]), aliases),
				Target: resolveName(unquote(m[2]), aliases),
				Kind:   "sync_call",
				Label:  strings.TrimSpace(m[3]),
			})
		}
	}
	return d, sc.Err()
}

func resolveName(name string, aliases map[string]string) string {
	if v, ok := aliases[name]; ok {
		return v
	}
	return name
}
