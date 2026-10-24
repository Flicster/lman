package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		path string
		p    Platform
		want InstallMethod
	}{
		{
			name: "homebrew formula",
			path: "/opt/homebrew/Cellar/go/1.25.1/libexec/bin/go",
			p:    Darwin,
			want: InstallMethod{Kind: MethodManager, Manager: "brew", Package: "go", Root: "/opt/homebrew/Cellar/go/1.25.1"},
		},
		{
			name: "nvm version",
			path: "/home/u/.nvm/versions/node/v22.3.0/bin/node",
			p:    Linux,
			want: InstallMethod{Kind: MethodManager, Manager: "nvm", Root: "/home/u/.nvm/versions/node/v22.3.0"},
		},
		{
			name: "rustup toolchain",
			path: "/home/u/.rustup/toolchains/stable-x86_64-unknown-linux-gnu/bin/rustc",
			p:    Linux,
			want: InstallMethod{
				Kind: MethodManager, Manager: "rustup", Package: "stable-x86_64-unknown-linux-gnu",
				Root: "/home/u/.rustup/toolchains/stable-x86_64-unknown-linux-gnu",
			},
		},
		{
			name: "linux system file waits for the package database",
			path: "/usr/bin/python3.12",
			p:    Linux,
			want: InstallMethod{Kind: MethodManager, System: true},
		},
		{
			name: "macOS developer tools",
			path: "/Library/Developer/CommandLineTools/usr/bin/python3",
			p:    Darwin,
			want: InstallMethod{Kind: MethodDirect, System: true},
		},
		{
			name: "archive in /usr/local",
			path: "/usr/local/go/bin/go",
			p:    Linux,
			want: InstallMethod{Kind: MethodDirect, Root: "/usr/local/go"},
		},
		{
			name: "jdk bundle",
			path: "/Library/Java/JavaVirtualMachines/temurin-21.jdk/Contents/Home/bin/java",
			p:    Darwin,
			want: InstallMethod{Kind: MethodDirect, Root: "/Library/Java/JavaVirtualMachines/temurin-21.jdk"},
		},
		{
			name: "file outside bin has no root",
			path: "/opt/tools/go",
			p:    Linux,
			want: InstallMethod{Kind: MethodDirect},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Classify(tt.path, tt.p))
		})
	}
}

func TestPlanRemoval(t *testing.T) {
	const home = "/home/u"
	tests := []struct {
		name     string
		version  Version
		p        Platform
		want     Removal
		wantHint string
	}{
		{
			name: "brew uninstalls the formula",
			version: Version{
				Number:   "1.25.1",
				RealPath: "/opt/homebrew/Cellar/go/1.25.1/libexec/bin/go",
				Method:   InstallMethod{Kind: MethodManager, Manager: "brew", Package: "go"},
			},
			p:    Darwin,
			want: Removal{Command: []string{"/opt/homebrew/bin/brew", "uninstall", "go"}},
		},
		{
			name: "rustup uninstalls the toolchain",
			version: Version{
				Method: InstallMethod{Kind: MethodManager, Manager: "rustup", Package: "nightly"},
			},
			p:    Linux,
			want: Removal{Command: []string{"/home/u/.cargo/bin/rustup", "toolchain", "uninstall", "nightly"}},
		},
		{
			name: "version manager directory is deleted",
			version: Version{
				Number: "3.12.4",
				Method: InstallMethod{Kind: MethodManager, Manager: "pyenv", Root: "/home/u/.pyenv/versions/3.12.4"},
			},
			p:    Linux,
			want: Removal{Dir: "/home/u/.pyenv/versions/3.12.4"},
		},
		{
			name: "direct install in home named after the version is deleted",
			version: Version{
				Number: "1.24.0",
				Method: InstallMethod{Kind: MethodDirect, Root: "/home/u/sdk/go1.24.0"},
			},
			p:    Linux,
			want: Removal{Dir: "/home/u/sdk/go1.24.0"},
		},
		{
			name: "direct install in home without the version is left to the user",
			version: Version{
				Number: "1.24.0",
				Method: InstallMethod{Kind: MethodDirect, Root: "/home/u/tools/go"},
			},
			p:        Linux,
			wantHint: "sudo rm -rf '/home/u/tools/go'",
		},
		{
			name: "top-level home folder is never deleted",
			version: Version{
				Number: "1",
				Method: InstallMethod{Kind: MethodDirect, Root: "/home/u/go1"},
			},
			p:        Linux,
			wantHint: "sudo rm -rf '/home/u/go1'",
		},
		{
			name: "apt package needs sudo",
			version: Version{
				Method: InstallMethod{Kind: MethodManager, Manager: "apt", Package: "golang-1.22-go", System: true},
			},
			p:        Linux,
			wantHint: "sudo apt remove golang-1.22-go",
		},
		{
			name: "system file without a package",
			version: Version{
				Method: InstallMethod{Kind: MethodDirect, System: true},
			},
			p: Darwin,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PlanRemoval(tt.version, tt.p, home)
			if tt.want.Command != nil || tt.want.Dir != "" {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
				return
			}
			var manual *ManualRemovalError
			require.ErrorAs(t, err, &manual)
			require.ErrorIs(t, err, ErrRemovalNotAllowed)
			assert.Equal(t, tt.wantHint, manual.Hint)
		})
	}
}

func TestInstallMethodString(t *testing.T) {
	assert.Equal(t, "brew (go)", InstallMethod{Manager: "brew", Package: "go"}.String())
	assert.Equal(t, "nvm", InstallMethod{Manager: "nvm"}.String())
	assert.Equal(t, "system", InstallMethod{System: true}.String())
	assert.Equal(t, "direct", InstallMethod{Kind: MethodDirect}.String())
}
