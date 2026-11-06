# Lman

Lman shows the programming languages installed on a computer: their versions, which version runs in the terminal, where the files are, how they were installed, and the environment variables that belong to them. The same view works over SSH on a Linux or macOS server.

There is a desktop window and a `lman` command for the terminal.

## Languages

Go, Python, JavaScript (Node.js), Java, Rust, Ruby, PHP, C, C++, C#, and Visual Basic. Markup languages are out of scope. `lman known` prints this list.

A version is something like `go1.26.5` or `3.12.4`. The **active** version is the one a new terminal runs. **not active** means none of the found installs is that program: the command is missing from `PATH`, or another program (for example a macOS installer stub) comes first.

To read a version, Lman runs that program once with its version flag. It only does this for absolute directories in `PATH` and for known install locations. A relative entry such as `.` or `node_modules/.bin` is ignored.

## Install

### Desktop application

Download the file for your system from the [latest release](../../releases/latest).

| System | File | How to open |
| --- | --- | --- |
| macOS, Apple Silicon | [`lman-darwin-arm64.dmg`](../../releases/latest/download/lman-darwin-arm64.dmg), archive [`lman-darwin-arm64.zip`](../../releases/latest/download/lman-darwin-arm64.zip) | Open the disk image and drag Lman to Applications. The zip contains `Lman.app`. |
| macOS, Intel | [`lman-darwin-amd64.dmg`](../../releases/latest/download/lman-darwin-amd64.dmg), archive [`lman-darwin-amd64.zip`](../../releases/latest/download/lman-darwin-amd64.zip) | Open the disk image and drag Lman to Applications. The zip contains `Lman.app`. |
| Windows | [`lman-windows-amd64.zip`](../../releases/latest/download/lman-windows-amd64.zip) | Unzip and open `lman-desktop.exe`. |
| Linux x86-64 | [`lman-linux-amd64.AppImage`](../../releases/latest/download/lman-linux-amd64.AppImage), archive [`lman-linux-amd64.tar.gz`](../../releases/latest/download/lman-linux-amd64.tar.gz) | `chmod +x` the AppImage and run it. In the archive, run `lman-desktop`. |

Linux needs GTK 3 and WebKitGTK 4.1, including for the AppImage. On Ubuntu: `sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0`. The AppImage also needs FUSE 2: `sudo apt install libfuse2` (Ubuntu 24.04: `libfuse2t64`). Or use [`lman-linux-amd64.tar.gz`](../../releases/latest/download/lman-linux-amd64.tar.gz). Windows uses the WebView2 runtime, which the application downloads on first launch if it is missing.

Each release includes `SHA256SUMS`. Check the file you downloaded before opening it:

```sh
grep ' lman-darwin-arm64.dmg$' SHA256SUMS | shasum -a 256 -c
```

Use the name of the file you downloaded. On Windows, `certutil -hashfile <file> SHA256` and compare the result with `SHA256SUMS`.

GitHub signs every built file. With the [GitHub CLI](https://cli.github.com/):

```sh
gh attestation verify lman-darwin-arm64.dmg --repo Flicster/lman
```

The application is not notarized and has no Apple or Microsoft certificate. macOS blocks a file downloaded from the internet the first time: open System Settings, then Privacy & Security, and choose **Open Anyway** next to the Lman message. On older macOS the same dialog offers **Open**. Windows SmartScreen asks for **More info**, then **Run anyway**. Do that only after the checksum and the attestation match.

The desktop window runs on macOS, Windows, and Linux. Remote servers are Linux and macOS only, reached with your existing SSH keys. Password prompts are disabled.

### Terminal command

`lman` is installed with Go. It is not in the release. You need [Go 1.26.10](https://go.dev/dl/) or newer.

```sh
go install github.com/Flicster/lman/cmd/lman@latest
```

`@latest` installs the newest version. A release tag selects that version:

```sh
go install github.com/Flicster/lman/cmd/lman@v1.2.3
```

The module path is `github.com/Flicster/lman`.

## Desktop

Open the Lman application.

The first screen is one tile per installed language. A tile shows the active version, or **not active**.

**Detectable languages** lists every language Lman knows how to find, including ones that are not installed.

Open a language to see its versions, paths, install method, and date, the tools around it, its environment variables, and the manual of the active version. From there you can make a version active, edit a variable, or remove a version or the whole language.

The host menu in the title bar starts on **local**. Connect with `user@server` or an alias from `~/.ssh/config`. **local** in that menu comes back to this computer.

The theme control is light, dark, or the system theme. The status bar shows the host, CPU, memory, and the Lman version. The clock is in the title bar.

## Terminal

```text
lman                                      installed languages
lman known                                languages Lman can detect
lman show <language>                      versions, paths, and tool hints
lman manual <language>                    manual of the active version
lman env <language>                       environment variables
lman env set <language> <name> <value>    set a variable ("" unsets it)
lman use <language> <version>             make a version active
lman remove [-y] <language> [<version>]   remove a version, or the language
lman status                               time, system, and Lman version
lman theme light|dark|system              theme of the desktop window
lman help

lman --remote <server> <command>          the same language commands over SSH
```

A language is named by its id or an alias: `go`, `python` (`py`), `javascript` (`js`, `node`), `java`, `rust`, `ruby`, `php`, `c` (`gcc`, `clang`), `cpp`, `csharp` (`dotnet`), `vb`.

Making a version active links its tools into `~/.lman/bin` and adds that directory to `PATH` for new terminals (a marked block in the shell profile, or the user `PATH` on Windows). Open a new terminal afterwards. `lman` does not change the shell you already have open.

Removal is conservative. A version that is active has to be switched away first. Files that belong to the operating system or a system package manager are not deleted; Lman prints the command you can run yourself.

`lman status` and `lman theme` always describe this computer, even if you pass `--remote`.

### Removing Lman

Lman does not install languages, so removing it leaves your installed versions alone. To remove everything Lman wrote on a computer (and on every server you connected to with `--remote` or the desktop window, since the same steps apply there):

1. Unset the variables Lman set while `lman` still works: `lman env <language>` lists them, and `lman env set <language> <name> ""` removes each one.
2. Remove the block between `# >>> lman >>>` and `# <<< lman <<<` from your shell profile: `~/.zshrc`, `~/.bashrc`, `~/.bash_profile` or `~/.profile`, depending on your shell. Do this first. Otherwise new terminals print an error because the block sources a file that no longer exists.
3. Delete `~/.lman` (on Windows, `%USERPROFILE%\.lman`).
4. Delete the desktop preferences: `lman/preferences.json` in the user configuration directory (`~/Library/Application Support` on macOS, `~/.config` on Linux, `%AppData%` on Windows). Delete the `lman` directory if it is empty afterwards.
5. On Windows, remove `%USERPROFILE%\.lman\bin` from the user `PATH` and remove the user variables Lman set (Settings, then Environment Variables). Lman keeps them in `HKEY_CURRENT_USER\Environment`.
6. Delete the application: the `Lman.app` bundle, the AppImage, or the `lman` binary you installed.

## Development

```sh
git clone https://github.com/Flicster/lman.git
cd lman
make pre-commit-lint # full cycle of checks
make test            # go test -race ./internal/...
make lint            # go vet and staticcheck
```

`make` writes `bin/lman` and `bin/lman-desktop` for local use. `make app` builds the application: `Lman.app` on macOS, `lman-desktop.exe` on Windows, `lman-desktop` on Linux. It needs the Wails CLI and a C compiler. On macOS that is the Xcode Command Line Tools (`xcode-select --install`).

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
make app
```

A pull request, and a commit on main, runs the tests and the linter on Windows, macOS, and Linux. Publishing a release creates the tag `v1.2.3`, and that tag is what builds the desktop application. The workflow does not create the tag or the release. It uploads the built files to the release you published: a `.dmg` on macOS, an `.AppImage` and a `.tar.gz` on Linux, a `.zip` on Windows. The terminal command is installed with `go install`.

## License

The source is public under the [PolyForm Noncommercial License 1.0.0](LICENSE.md) (`PolyForm-Noncommercial-1.0.0`).

You may use, study, change, and share Lman free of charge for a noncommercial purpose. That includes personal use, study, experiments, and hobby projects, and use by schools, charities, public research organizations, and government institutions.

Any commercial purpose needs a separate license that you buy from the copyright holder. That includes use in a business and shipping Lman as part of something you sell. To buy a commercial license, contact the copyright holder through the contact on their GitHub profile, or open an issue in this repository.

This is a source-available license. It is not an Open Source license under the [Open Source Definition](https://opensource.org/osd), because that definition does not allow a separate fee for commercial use. The summary in this section is not the license. The license is [LICENSE.md](LICENSE.md).

Required Notice: Copyright (c) 2026 the copyright holder of Lman. Commercial use requires a separate license purchased from the copyright holder.

## Поддержать проект ❤️

Если этот проект помог вам или вы хотите поддержать его развитие, вы можете отправить криптовалюту:

### Криптовалюта:
* **USDT (TRC-20):** `TH4FLpwa3paKkwiQSfDHNZNzKfmginaFCn`
