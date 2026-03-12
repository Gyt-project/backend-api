package scalars

import (
	"fmt"
	"io"
	"time"

	"github.com/99designs/gqlgen/graphql"
)

// Time is a custom GraphQL scalar that maps to time.Time (RFC3339).
type Time = time.Time

func MarshalTime(t time.Time) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		_, _ = io.WriteString(w, `"`+t.UTC().Format(time.RFC3339)+`"`)
	})
}

func UnmarshalTime(v interface{}) (time.Time, error) {
	switch val := v.(type) {
	case string:
		t, err := time.Parse(time.RFC3339, val)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid Time format, expected RFC3339: %w", err)
		}
		return t, nil
	default:
		return time.Time{}, fmt.Errorf("Time must be a RFC3339 string, got %T", v)
	}
}

