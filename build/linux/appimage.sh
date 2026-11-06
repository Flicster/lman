#!/bin/sh
# Build a one-file AppImage around the Wails binary. GTK 3 and WebKitGTK 4.1
# stay on the system, the same as for the plain binary.
set -eu

if [ "$#" -ne 3 ]; then
	echo "usage: appimage.sh <binary> <icon.png> <output.AppImage>" >&2
	exit 2
fi

binary="$1"
icon="$2"
output="$3"

appdir="$(mktemp -d)"
trap 'rm -rf "$appdir"' EXIT

mkdir -p "$appdir/usr/bin"
cp "$binary" "$appdir/usr/bin/lman-desktop"
chmod +x "$appdir/usr/bin/lman-desktop"
cp "$icon" "$appdir/lman.png"

cat > "$appdir/lman.desktop" <<'EOF'
[Desktop Entry]
Type=Application
Name=Lman
Comment=Programming languages on your computer
Exec=lman-desktop
Icon=lman
Categories=Development;
Terminal=false
EOF

cat > "$appdir/AppRun" <<'EOF'
#!/bin/sh
HERE="$(dirname "$(readlink -f "$0")")"
exec "$HERE/usr/bin/lman-desktop" "$@"
EOF
chmod +x "$appdir/AppRun"

# Pinned release. The checksum is the trust anchor; the download URL is not.
tool="${RUNNER_TEMP:-/tmp}/appimagetool-1.9.1-x86_64.AppImage"
url="https://github.com/AppImage/appimagetool/releases/download/1.9.1/appimagetool-x86_64.AppImage"
sum="ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0"
curl -fsSL -o "$tool" "$url"
echo "$sum  $tool" | sha256sum -c -
chmod +x "$tool"

ARCH=x86_64 "$tool" --appimage-extract-and-run "$appdir" "$output"
