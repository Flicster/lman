// Package shellenv owns the part of the shell configuration Lman changes: a directory
// of tool links placed first in PATH and environment variables for new terminals.
package shellenv

import (
	"encoding/json"
	"fmt"
)

// state is what Lman set; files in the shim directory and the profile are derived from it.
type state struct {
	Links map[string]string `json:"links"`
	Vars  map[string]string `json:"vars"`
}

func decodeState(data []byte) (state, error) {
	st := state{Links: map[string]string{}, Vars: map[string]string{}}
	if len(data) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return state{}, fmt.Errorf("decode state: %w", err)
	}
	if st.Links == nil {
		st.Links = map[string]string{}
	}
	if st.Vars == nil {
		st.Vars = map[string]string{}
	}
	return st, nil
}

func (st state) encode() ([]byte, error) {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode state: %w", err)
	}
	return append(data, '\n'), nil
}
