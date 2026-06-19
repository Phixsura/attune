package attune

import (
	"regexp"
	"testing"
)

// TestVersionIsSemver pins Version to a MAJOR.MINOR.PATCH(-prerelease) shape.
// The release workflow (sdk-go-release.yml) enforces that the `sdk/go/vX.Y.Z`
// tag matches this constant, so this is the single source the tag is checked
// against — keeping it well-formed prevents a malformed tag/release.
func TestVersionIsSemver(t *testing.T) {
	semver := regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)
	if !semver.MatchString(Version) {
		t.Errorf("Version %q is not semver MAJOR.MINOR.PATCH", Version)
	}
}
