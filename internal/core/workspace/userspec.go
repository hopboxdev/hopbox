package workspace

import (
	"fmt"
	"strings"
	"time"
)

// UserSpec is the parsed form of an SSH-front-door username, the krillbox-style
// grammar:
//
//	workspace[~backend][:image[:flavor[+duration]]]
//
// Parsed left-to-right, colon-delimited; `~` attaches a backend to the workspace
// segment; a trailing `+` on the workspace forces a fresh box; a `+duration`
// suffix on the last segment is the stay-alive grace after disconnect.
//
// Special usernames (cli, sudo, _, session-<id>) spawn no box: Special is set
// and the spec fields are empty.
type UserSpec struct {
	Workspace string
	Backend   string        // "" = auto (resolved later via ResolveBackend)
	Image     string        // "" = caller's default image
	Flavor    string        // hardware flavor; not yet applied (no compute-flavor field)
	Grace     time.Duration // stay-alive after disconnect; 0 = die immediately
	ForceNew  bool          // workspace name ended with '+'
	Special   string        // "cli" | "sudo" | "_" | "session"; "" = normal box
	SessionID string        // set when Special == "session"
}

// ParseSpec parses an SSH username into a UserSpec.
func ParseSpec(username string) (UserSpec, error) {
	if username == "" {
		return UserSpec{}, fmt.Errorf("empty username")
	}
	switch username {
	case "cli", "sudo":
		return UserSpec{Special: username}, nil
	case "_":
		return UserSpec{Special: "_"}, nil
	}
	if id, ok := strings.CutPrefix(username, "session-"); ok {
		return UserSpec{Special: "session", SessionID: id}, nil
	}

	var s UserSpec
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
				return UserSpec{}, fmt.Errorf("bad duration %q: %w", suffix, err)
			}
			s.Grace = d
		}
	}

	segs := strings.Split(rest, ":")
	// Segment 0 carries the workspace and an optional ~backend.
	ws, backend, _ := strings.Cut(segs[0], "~")
	if ws == "" {
		return UserSpec{}, fmt.Errorf("empty workspace name in %q", username)
	}
	s.Workspace, s.Backend = ws, backend
	if len(segs) > 1 {
		s.Image = segs[1]
	}
	if len(segs) > 2 {
		s.Flavor = segs[2]
	}
	if len(segs) > 3 {
		return UserSpec{}, fmt.Errorf("too many segments in %q", username)
	}
	return s, nil
}

// BuildWorkspace turns a parsed box spec into a desired Workspace. SSH-spawned
// boxes are temporary (krillbox semantics): always Ephemeral, with Grace from
// the parsed duration (0 = reap on disconnect). The backend is resolved against
// what is actually configured. It errors on special usernames, which spawn no box.
func (s UserSpec) BuildWorkspace(tenantID, owner, defaultImage string, backends []string, defBackend string) (*Workspace, error) {
	if s.Special != "" {
		return nil, fmt.Errorf("special username %q spawns no workspace", s.Special)
	}
	backend, err := ResolveBackend(s.Backend, backends, defBackend)
	if err != nil {
		return nil, err
	}
	image := s.Image
	if image == "" {
		image = defaultImage
	}
	w := New(tenantID, owner, s.Workspace, image)
	w.Backend = backend
	w.Ephemeral = true
	w.Grace = s.Grace
	return w, nil
}
