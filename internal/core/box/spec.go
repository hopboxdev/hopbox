package box

import (
	"fmt"
	"strings"
	"time"
)

// Spec is the parsed form of a box request — the `ssh box@host` username grammar:
//
//	name[~backend][:image[:flavor[+duration]]]
//
// Parsed left-to-right, colon-delimited; `~` attaches a backend to the name
// segment; a trailing `+` on the name forces a fresh box; a `+duration` suffix on
// the last segment is the stay-alive grace after disconnect.
//
// Special usernames (cli, sudo, _, session-<id>) spawn no box: Special is set and
// the spec fields are empty.
type Spec struct {
	Name      string
	Backend   string        // "" = auto (resolved later via ResolveBackend)
	Image     string        // "" = caller's default image
	Flavor    string        // hardware flavor; resolved to a Flavor later
	Grace     time.Duration // stay-alive after disconnect; 0 = die immediately
	ForceNew  bool          // name ended with '+'
	Special   string        // "cli" | "sudo" | "_" | "session"; "" = normal box
	SessionID string        // set when Special == "session"
}

// ParseSpec parses an SSH username into a Spec.
func ParseSpec(username string) (Spec, error) {
	if username == "" {
		return Spec{}, fmt.Errorf("empty username")
	}
	switch username {
	case "cli", "sudo":
		return Spec{Special: username}, nil
	case "_":
		return Spec{Special: "_"}, nil
	}
	if id, ok := strings.CutPrefix(username, "session-"); ok {
		return Spec{Special: "session", SessionID: id}, nil
	}

	var s Spec
	rest := username

	// Trailing '+duration' on the whole string: a '+' followed by a non-empty
	// token is a duration; a bare trailing '+' is the force-new modifier.
	if i := strings.LastIndex(rest, "+"); i >= 0 {
		suffix := rest[i+1:]
		rest = rest[:i]
		if suffix == "" {
			s.ForceNew = true
		} else {
			d, err := time.ParseDuration(suffix)
			if err != nil {
				return Spec{}, fmt.Errorf("bad duration %q: %w", suffix, err)
			}
			s.Grace = d
		}
	}

	segs := strings.Split(rest, ":")
	// Segment 0 carries the box name and an optional ~backend.
	name, backend, _ := strings.Cut(segs[0], "~")
	if name == "" {
		return Spec{}, fmt.Errorf("empty box name in %q", username)
	}
	s.Name, s.Backend = name, backend
	if len(segs) > 1 {
		s.Image = segs[1]
	}
	if len(segs) > 2 {
		s.Flavor = segs[2]
	}
	if len(segs) > 3 {
		return Spec{}, fmt.Errorf("too many segments in %q", username)
	}
	return s, nil
}
