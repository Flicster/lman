package shellenv

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validLinkName is a single file name. A slash or ".." would make the shim path
// leave ~/.lman/bin when state.json was edited by hand.
func validLinkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, 0) {
		return fmt.Errorf("invalid tool name %q", name)
	}
	if strings.ContainsAny(name, `/\`) || filepath.Base(name) != name {
		return fmt.Errorf("invalid tool name %q", name)
	}
	return nil
}

// shimFile is the name placed on PATH. The target extension is kept, so a batch
// program stays a link to that program instead of a new .cmd that re-parses arguments.
func shimFile(name, target string) string {
	switch strings.ToLower(filepath.Ext(target)) {
	case ".exe", ".cmd", ".bat", ".com":
		return name + strings.ToLower(filepath.Ext(target))
	default:
		return name + ".exe"
	}
}
