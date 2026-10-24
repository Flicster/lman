# Lman

Lman shows the programming languages installed on a computer: their versions, which version runs in the terminal, where the files are, how they were installed, and the environment variables that belong to them. The same view works over SSH on a Linux or macOS server.

There is a desktop window and a `lman` command for the terminal.

## Languages

Go, Python, JavaScript (Node.js), Java, Rust, Ruby, PHP, C, C++, C#, and Visual Basic. Markup languages are out of scope. `lman known` prints this list.

A version is something like `go1.26.5` or `3.12.4`. The **active** version is the one a new terminal runs. **not active** means none of the found installs is that program: the command is missing from `PATH`, or another program (for example a macOS installer stub) comes first.

## Install

### Desktop application

Download the file for your system. These links always open the [latest release](../../releases/latest). Every version, with its number in the file name, is in [Releases](../../releases).

| System | File | How to open |
| --- | --- | --- |
| macOS, Apple Silicon | [`lman-darwin-arm64.dmg`](../../releases/latest/download/lman-darwin-arm64.dmg) | Open it and drag Lman to Applications. The same application is also a [`.zip`](../../releases/latest/download/lman-darwin-arm64.zip). |
| macOS, Intel | [`lman-darwin-amd64.dmg`](../../releases/latest/download/lman-darwin-amd64.dmg) | Open it and drag Lman to Applications. The same application is also a [`.zip`](../../releases/latest/download/lman-darwin-amd64.zip). |
| Windows | [`lman-windows-amd64.zip`](../../releases/latest/download/lman-windows-amd64.zip) | Unzip and open `lman-desktop.exe`. |
| Linux x86-64 | [`lman-linux-amd64.AppImage`](../../releases/latest/download/lman-linux-amd64.AppImage) | `chmod +x lman-linux-amd64.AppImage` and run it. The same program as a folder: [`lman-linux-amd64.tar.gz`](../../releases/latest/download/lman-linux-amd64.tar.gz), run `lman-desktop` inside. |

Linux needs GTK 3 and WebKitGTK 4.1, including for the AppImage. On Ubuntu: `sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0`. The AppImage also needs FUSE 2: `sudo apt install libfuse2` (Ubuntu 24.04: `libfuse2t64`). Or use the `.tar.gz`. Windows uses the WebView2 runtime, which the application downloads on first launch if it is missing.

To check a file, compare its sha256 with the `.sha256` file beside it: `certutil -hashfile <file> SHA256` on Windows, `shasum -a 256 <file>` on macOS, `sha256sum <file>` on Linux.

The application is not signed. macOS asks you to open it from the context menu the first time. Windows SmartScreen asks for **More info**, then **Run anyway**.

The desktop window runs on macOS, Windows, and Linux. Remote servers are Linux and macOS only, reached with your existing SSH keys. Password prompts are disabled.

### Terminal command

`lman` is installed with Go. It is not in the release. You need [Go 1.26.5](https://go.dev/dl/) or newer.

```sh
go install github.com/<owner>/lman/cmd/lman@latest
```

`@latest` installs the newest version. A release tag selects that version:

```sh
go install github.com/<owner>/lman/cmd/lman@v1.2.3
```

`go install` requires the module path to be this repository, `github.com/<owner>/lman`.

## Desktop

Open the Lman application.

The first screen is one tile per installed language. The tile size stays the same when you resize the window; the tiles reflow down to a single column. A tile shows the active version, or **not active**.

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

## Development

```sh
git clone https://github.com/<owner>/lman.git
cd lman
make test    # go test -race ./internal/...
make lint    # go vet and staticcheck
```

`make` writes `bin/lman` and `bin/lman-desktop` for local use. `make app` builds the application: `Lman.app` on macOS, `lman-desktop.exe` on Windows, `lman-desktop` on Linux. It needs the Wails CLI and a C compiler. On macOS that is the Xcode Command Line Tools (`xcode-select --install`).

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
make app
```

Every push and pull request runs the tests and the linter on Windows, macOS, and Linux. A push to `main` builds the desktop application. A tag `v1.2.3` publishes that version: a `.dmg` on macOS, an `.AppImage` and a `.tar.gz` on Linux, a `.zip` on Windows, each with a `.sha256`. The terminal command is installed with `go install`.

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
