package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFind(t *testing.T) {
	lang := Language{
		Definition: Definition{Name: "Go", VersionPrefix: "go"},
		Versions: []Version{
			{Number: "1.25.1", Path: "/usr/local/go/bin/go", RealPath: "/usr/local/go/bin/go"},
			{Number: "1.24.0", Path: "/home/u/sdk/go1.24.0/bin/go", RealPath: "/home/u/sdk/go1.24.0/bin/go"},
			{Number: "1.22.5", Path: "/a/bin/go", RealPath: "/a/bin/go"},
			{Number: "1.22.5", Path: "/b/bin/go", RealPath: "/b/bin/go"},
		},
	}
	tests := []struct {
		query   string
		want    string
		wantErr error
	}{
		{query: "1.25.1", want: "/usr/local/go/bin/go"},
		{query: "go1.24.0", want: "/home/u/sdk/go1.24.0/bin/go"},
		{query: "go1.25", want: "/usr/local/go/bin/go"},
		{query: "/b/bin/go", want: "/b/bin/go"},
		{query: "1.2", wantErr: ErrVersionNotFound},
		{query: "1.22.5", wantErr: ErrAmbiguousVersion},
		{query: "1", wantErr: ErrAmbiguousVersion},
		{query: " ", wantErr: ErrVersionNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			v, err := lang.Find(tt.query)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, v.Path)
		})
	}
}

func TestCompareVersions(t *testing.T) {
	assert.Positive(t, CompareVersions("1.10.0", "1.9.9"))
	assert.Negative(t, CompareVersions("3.9.6", "3.12.1"))
	assert.Positive(t, CompareVersions("21.0.2", "1.8.0_392"))
	assert.Positive(t, CompareVersions("1.25", "1.24.9"))
	assert.Zero(t, CompareVersions("1.2.3", "1.2.3"))
}

func TestLocations(t *testing.T) {
	d := Definition{Search: []string{
		"/usr/local/go/bin/go",
		"~/sdk/go*/bin/go",
		"{brew}/Cellar/go/*/bin/go",
		"{programfiles}/Go/bin/go",
	}}

	assert.Equal(t, []string{
		"/usr/local/go/bin/go",
		"/Users/u/sdk/go*/bin/go",
		"/opt/homebrew/Cellar/go/*/bin/go",
		"/usr/local/Cellar/go/*/bin/go",
	}, d.Locations(Darwin, "/Users/u", nil))

	assert.Equal(t, []string{
		"C:/Users/u/sdk/go*/bin/go.exe",
		"C:/Program Files/Go/bin/go.exe",
	}, d.Locations(Windows, "C:/Users/u", map[string]string{"ProgramFiles": `C:\Program Files`}))
}

func TestCatalogParsesVersions(t *testing.T) {
	outputs := map[LanguageID]struct{ output, want string }{
		"go":         {output: "go version go1.26rc1 darwin/arm64", want: "1.26rc1"},
		"python":     {output: "Python 3.12.4\n", want: "3.12.4"},
		"javascript": {output: "v22.3.0\n", want: "22.3.0"},
		"java":       {output: "openjdk version \"21.0.2\" 2024-01-16\nOpenJDK Runtime", want: "21.0.2"},
		"rust":       {output: "rustc 1.80.0 (051478957 2024-07-21)", want: "1.80.0"},
		"ruby":       {output: "ruby 2.6.10p210 (2022-04-12 revision 67958)", want: "2.6.10"},
		"php":        {output: "PHP 8.3.1 (cli) (built: Dec 20 2023)", want: "8.3.1"},
		"c":          {output: "Apple clang version 16.0.0 (clang-1600.0.26.6)\n", want: "16.0.0"},
		"cpp":        {output: "g++ (Ubuntu 13.2.0-23ubuntu4) 13.2.0\n", want: "13.2.0"},
		"csharp":     {output: "8.0.404\n", want: "8.0.404"},
		"vb":         {output: "Microsoft (R) Visual Basic Compiler version 4.8.0\n", want: "4.8.0"},
	}
	for _, d := range Catalog() {
		t.Run(string(d.ID), func(t *testing.T) {
			tt, ok := outputs[d.ID]
			require.True(t, ok, "add a sample output for %s", d.ID)
			got, ok := d.ParseVersion(tt.output)
			require.True(t, ok)
			assert.Equal(t, tt.want, got)
			assert.NotEmpty(t, d.Primaries)
			assert.True(t, d.IsBinary(d.Primaries[0]), "the primary executable must match Binary")
		})
	}
}

func TestDefinitionRules(t *testing.T) {
	py := Catalog()[1]
	assert.True(t, py.Matches("PY"))
	assert.True(t, py.IsBinary("python3.12"))
	assert.True(t, py.IsBinary("python.exe"))
	assert.False(t, py.IsBinary("python3.12-config"))
	assert.True(t, py.IsStub(Darwin, "/usr/bin/python3"))
	assert.False(t, py.IsStub(Linux, "/usr/bin/python3"))
	assert.True(t, py.OwnsVar("PYTHONPATH"))
	assert.False(t, py.OwnsVar("PATH"))

	c := definition("c")
	assert.True(t, c.IsBinary("clang-18"))
	assert.False(t, c.IsBinary("clang++"), "the C++ compiler is a different language")
	assert.False(t, c.IsBinary("clang-tidy"))
	cpp := definition("cpp")
	assert.True(t, cpp.IsBinary("g++-14"))
	assert.True(t, cpp.IsBinary("clang++"))
	vb := definition("vb")
	assert.True(t, vb.IsBinary("vbc.dll"))
	number, ok := vb.NumberFromPath("/home/u/.dotnet/sdk/8.0.404/Roslyn/bincore/vbc.dll")
	assert.True(t, ok)
	assert.Equal(t, "8.0.404", number)

	assert.True(t, IsProxy("/home/u/.cargo/bin/rustc"))
	assert.False(t, IsProxy("/home/u/.rustup/toolchains/stable/bin/rustc"))

	require.NoError(t, ValidateVarName("GO_FLAGS1"))
	require.ErrorIs(t, ValidateVarName("1GO"), ErrInvalidVarName)
	require.ErrorIs(t, ValidateVarName("A B"), ErrInvalidVarName)
}

func definition(id LanguageID) Definition {
	for _, d := range Catalog() {
		if d.ID == id {
			return d
		}
	}
	return Definition{}
}

func TestParseTheme(t *testing.T) {
	theme, err := ParseTheme("dark")
	require.NoError(t, err)
	assert.Equal(t, ThemeDark, theme)
	_, err = ParseTheme("blue")
	require.ErrorIs(t, err, ErrInvalidTheme)
}
