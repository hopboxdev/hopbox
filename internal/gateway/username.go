package gateway

import "strings"

// ParseUsername splits an SSH username like "hop+boxname" into user and boxname.
// If no "+" separator or boxname is empty, boxname defaults to "default".
func ParseUsername(raw string) (user, boxname string) {
	parts := strings.SplitN(raw, "+", 2)
	user = parts[0]
	if len(parts) == 2 && parts[1] != "" {
		boxname = parts[1]
	} else {
		boxname = "default"
	}
	return user, boxname
}
