package domain

import (
	"os/exec"
	"runtime"
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
			assert.Equal(t, tt.want, Classify(tt.path, tt.p, "/home/u"))
		})
	}
}

func TestClassifyOnlyTrustsKnownRoots(t *testing.T) {
	tests := []struct {
		name string
		path string
		p    Platform
		home string
		want InstallMethod
	}{
		{
			name: "cellar outside a homebrew prefix is not brew",
			path: "/tmp/x/Cellar/go/1.25.1/bin/go",
			p:    Darwin,
			home: "/home/u",
			want: InstallMethod{Kind: MethodDirect, Root: "/tmp/x/Cellar/go/1.25.1"},
		},
		{
			name: "version manager directory outside home is not managed",
			path: "/home/u/work/.nvm/versions/node/v22.3.0/bin/node",
			p:    Linux,
			home: "/home/u",
			want: InstallMethod{Kind: MethodDirect, Root: "/home/u/work/.nvm/versions/node/v22.3.0"},
		},
		{
			name: "no home means no home manager",
			path: "/home/u/.pyenv/versions/3.12.4/bin/python3",
			p:    Linux,
			home: "",
			want: InstallMethod{Kind: MethodDirect, Root: "/home/u/.pyenv/versions/3.12.4"},
		},
		{
			name: "jdk bundle is anchored at .jdk",
			path: "/Users/u/work/my.jdk/Contents/Home/bin/java",
			p:    Darwin,
			home: "/Users/u",
			want: InstallMethod{Kind: MethodDirect, Root: "/Users/u/work/my.jdk"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Classify(tt.path, tt.p, tt.home))
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
			name: "brew deletes the keg of this version",
			version: Version{
				Number:   "1.25.1",
				RealPath: "/opt/homebrew/Cellar/go/1.25.1/libexec/bin/go",
				Method:   InstallMethod{Kind: MethodManager, Manager: "brew", Package: "go"},
			},
			p:    Darwin,
			want: Removal{Dir: "/opt/homebrew/Cellar/go/1.25.1"},
		},
		{
			name: "brew formula rack is not a keg",
			version: Version{
				Number:   "1.25.1",
				RealPath: "/opt/homebrew/Cellar/go",
				Method:   InstallMethod{Kind: MethodManager, Manager: "brew", Package: "go"},
			},
			p: Darwin,
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
			name: "project directory named after the version is left in place",
			version: Version{
				Number: "1.24.0",
				Method: InstallMethod{Kind: MethodDirect, Root: "/home/u/projects/go1.24.0"},
			},
			p: Linux,
		},
		{
			name: "direct install in home without the version is left to the user",
			version: Version{
				Number: "1.24.0",
				Method: InstallMethod{Kind: MethodDirect, Root: "/home/u/tools/go"},
			},
			p: Linux,
		},
		{
			name: "top-level home folder is never deleted",
			version: Version{
				Number: "1",
				Method: InstallMethod{Kind: MethodDirect, Root: "/home/u/go1"},
			},
			p: Linux,
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
		{
			name: "jdk directory named with the version is deleted",
			version: Version{
				Number: "21",
				Method: InstallMethod{Kind: MethodDirect, Root: "/home/u/sdk/jdk-21"},
			},
			p:    Linux,
			want: Removal{Dir: "/home/u/sdk/jdk-21"},
		},
		{
			name: "short version does not own an unrelated directory",
			version: Version{
				Number: "21",
				Method: InstallMethod{Kind: MethodDirect, Root: "/home/u/work/client-21"},
			},
			p: Linux,
		},
		{
			name: "binary in /usr/local/bin does not target /usr/local",
			version: Version{
				Number: "20.1.0",
				Method: InstallMethod{Kind: MethodDirect, Root: "/usr/local"},
			},
			p: Linux,
		},
		{
			name: "binary in ~/bin does not target the home directory",
			version: Version{
				Number: "1.22.3",
				Method: InstallMethod{Kind: MethodDirect, Root: "/home/u"},
			},
			p: Linux,
		},
		{
			name: "binary in ~/.local/bin does not target ~/.local",
			version: Version{
				Number: "3.12.1",
				Method: InstallMethod{Kind: MethodDirect, Root: "/home/u/.local"},
			},
			p: Linux,
		},
		{
			name: "traversal into a shared prefix is refused",
			version: Version{
				Number: "1.24.0",
				Method: InstallMethod{Kind: MethodDirect, Root: "/home/u/../../usr/local"},
			},
			p: Linux,
		},
		{
			name: "manual command quotes a posix metacharacter",
			version: Version{
				Number: "1.24.0",
				Method: InstallMethod{Kind: MethodDirect, Root: "/opt/x'; touch pwned; echo '/go1.24.0"},
			},
			p:        Linux,
			wantHint: "sudo rm -rf '/opt/x'\\''; touch pwned; echo '\\''/go1.24.0'",
		},
		{
			name: "brew outside a homebrew prefix is not run",
			version: Version{
				Number:   "1.25.1",
				RealPath: "/tmp/x/Cellar/go/1.25.1/libexec/bin/go",
				Method:   InstallMethod{Kind: MethodManager, Manager: "brew", Package: "go"},
			},
			p: Darwin,
		},
		{
			name: "package name that reads as an option is refused",
			version: Version{
				Number:   "1.25.1",
				RealPath: "/opt/homebrew/Cellar/go/1.25.1/libexec/bin/go",
				Method:   InstallMethod{Kind: MethodManager, Manager: "brew", Package: "--force"},
			},
			p: Darwin,
		},
		{
			name: "apt name with shell syntax is refused",
			version: Version{
				Method: InstallMethod{Kind: MethodManager, Manager: "apt", Package: "x; reboot", System: true},
			},
			p: Linux,
		},
		{
			name: "windows hint does not expand powershell",
			version: Version{
				Number: "1.24.0",
				Method: InstallMethod{Kind: MethodDirect, Root: "C:/Tools/$(Start-Process calc)/go1.24.0"},
			},
			p:        Windows,
			wantHint: "Remove-Item -Recurse -LiteralPath 'C:/Tools/$(Start-Process calc)/go1.24.0' as administrator",
		},
		{
			name: "windows hint quotes a single quote",
			version: Version{
				Number: "1.24.0",
				Method: InstallMethod{Kind: MethodDirect, Root: "C:/Tools/o'brien/go1.24.0"},
			},
			p:        Windows,
			wantHint: "Remove-Item -Recurse -LiteralPath 'C:/Tools/o''brien/go1.24.0' as administrator",
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

func TestPosixQuoteRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell")
	}
	raw := "/opt/x'; touch pwned; echo '"
	out, err := exec.Command("sh", "-c", "printf %s "+posixQuote(raw)).Output()
	require.NoError(t, err)
	assert.Equal(t, raw, string(out))
}

func TestInstallMethodString(t *testing.T) {
	assert.Equal(t, "brew (go)", InstallMethod{Manager: "brew", Package: "go"}.String())
	assert.Equal(t, "nvm", InstallMethod{Manager: "nvm"}.String())
	assert.Equal(t, "system", InstallMethod{System: true}.String())
	assert.Equal(t, "direct", InstallMethod{Kind: MethodDirect}.String())
}
