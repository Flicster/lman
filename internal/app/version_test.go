package app

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppVersion(t *testing.T) {
	tests := []struct {
		linked string
		want   string
	}{
		{linked: "v1.2.3", want: "1.2.3"},
		{linked: "1.2.3", want: "1.2.3"},
		{linked: "v1.2.3-rc.1", want: "1.2.3-rc.1"},
	}
	for _, tt := range tests {
		t.Run(tt.linked, func(t *testing.T) {
			assert.Equal(t, tt.want, AppVersion(tt.linked))
		})
	}
}

func TestAppVersionDevFallsBack(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	require.True(t, ok)
	want := "dev"
	if v := plainVersion(info.Main.Version); v != "" {
		want = v
	}
	assert.Equal(t, want, AppVersion("dev"))
	assert.Equal(t, want, AppVersion(""))
}
