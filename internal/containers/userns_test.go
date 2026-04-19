package containers

import (
	"strings"
	"testing"
)

func TestParseMountInfoIdmapped(t *testing.T) {
	// Real mountinfo sample from a Sysbox container with idmapped bind
	// mount for /home/dev, plus a couple of sibling mounts without the
	// flag so we know the parser isn't matching too eagerly.
	const sample = `
595 479 0:58 / / rw,relatime - overlay overlay rw,lowerdir=/var/lib/docker/overlay2/...
596 595 0:62 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw
609 593 9:3 /var/lib/hopbox/users/SHA256_x/boxes/hopbox/home /home/dev rw,relatime,idmapped - ext4 /dev/md3 rw
612 595 9:3 /var/lib/something-else /opt/other rw,relatime - ext4 /dev/md3 rw
`
	tests := []struct {
		name  string
		point string
		want  bool
	}{
		{"idmapped bind", "/home/dev", true},
		{"trailing slash still matches", "/home/dev/", true},
		{"non-idmapped mount", "/opt/other", false},
		{"unmounted path", "/nonexistent", false},
		{"root overlay not idmapped", "/", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMountInfoIdmapped(strings.NewReader(sample), tc.point)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseMountInfoIdmapped(%q) = %v, want %v", tc.point, got, tc.want)
			}
		})
	}
}

func TestParseMountInfoIdmapped_OptionalFields(t *testing.T) {
	// mountinfo optional fields (shared:N, master:N, …) appear between
	// mount-options (field 5) and the "-" separator. The parser only
	// looks at field 5, so these should not confuse it.
	const sample = `42 41 0:1 / /home/dev rw,relatime,idmapped shared:1 master:2 - ext4 /dev/sda1 rw
`
	got, err := parseMountInfoIdmapped(strings.NewReader(sample), "/home/dev")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !got {
		t.Errorf("expected idmapped=true with optional fields present")
	}
}

func TestParseMountInfoIdmapped_SubstringNotMatched(t *testing.T) {
	// "idmapped-foo" would match a naive substring check but is not the
	// kernel-set flag.
	const sample = `42 41 0:1 / /home/dev rw,relatime,idmapped-foo - ext4 /dev/sda1 rw
`
	got, err := parseMountInfoIdmapped(strings.NewReader(sample), "/home/dev")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got {
		t.Errorf("expected idmapped=false when only a substring-prefixed option is present")
	}
}
