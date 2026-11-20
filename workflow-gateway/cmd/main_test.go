package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseInstanceCacheSize(t *testing.T) {
	for input, want := range map[string]int64{"20Mi": 20 << 20, "100Mi": 100 << 20, "1.5Ki": 1536, "384": 384} {
		got, err := parseInstanceCacheSize(input)
		require.NoError(t, err, input)
		require.Equal(t, want, got, input)
	}
	for _, input := range []string{"", "abc", "0", "-1Mi", "0.5", "9223372036854775808"} {
		_, err := parseInstanceCacheSize(input)
		require.Error(t, err, input)
	}
}
