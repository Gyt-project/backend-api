package service

import (
	"regexp"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// validNameRe accepts only lowercase letters, digits, and hyphens.
// Leading/trailing hyphens and consecutive hyphens are also rejected.
var validNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// validateIdentifier checks that name (already lowercased and trimmed) is a
// safe identifier for usernames, organization names, and repository names.
//
// Rules:
//   - Only a–z, 0–9, and hyphens.
//   - Must start and end with a letter or digit.
//   - No consecutive hyphens.
//   - Length 1–64 characters.
func validateIdentifier(kind, name string) error {
	if len(name) == 0 {
		return status.Errorf(codes.InvalidArgument, "%s name is required", kind)
	}
	if len(name) > 64 {
		return status.Errorf(codes.InvalidArgument, "%s name must be 64 characters or fewer", kind)
	}
	if !validNameRe.MatchString(name) {
		return status.Errorf(codes.InvalidArgument,
			"%s name may only contain lowercase letters, digits, and hyphens, "+
				"and must start and end with a letter or digit",
			kind,
		)
	}
	if strings.Contains(name, "--") {
		return status.Errorf(codes.InvalidArgument, "%s name must not contain consecutive hyphens", kind)
	}
	return nil
}
