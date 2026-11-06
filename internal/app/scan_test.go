package app

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Flicster/lman/internal/domain"
)

func TestPathDirsDropsRelativeEntries(t *testing.T) {
	assert.Equal(t, []string{"/usr/bin", "/home/u/bin"},
		pathDirs("/usr/bin:.:bin:node_modules/.bin:/home/u/bin:", domain.Linux))
	assert.Equal(t, []string{"/usr/bin"}, pathDirs("/usr/bin:/usr/bin", domain.Linux))
	assert.Equal(t, []string{"C:/Go/bin", "D:/Tools"},
		pathDirs(`C:\Go\bin;.\bin;D:/Tools`, domain.Windows))
	assert.Equal(t, []string{"//server/share/bin"}, pathDirs(`//server/share/bin`, domain.Windows))
}
