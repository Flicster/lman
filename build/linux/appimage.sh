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

tool="${RUNNER_TEMP:-/tmp}/appimagetool-x86_64.AppImage"
python3 - "$tool" <<'PY'
import hashlib
import json
import os
import stat
import sys
import urllib.request

dest = sys.argv[1]
api = "https://api.github.com/repos/AppImage/appimagetool/releases/tags/continuous"
req = urllib.request.Request(
    api,
    headers={"User-Agent": "lman", "Accept": "application/vnd.github+json"},
)
with urllib.request.urlopen(req, timeout=60) as resp:
    rel = json.load(resp)
asset = next(a for a in rel["assets"] if a["name"] == "appimagetool-x86_64.AppImage")
with urllib.request.urlopen(asset["browser_download_url"], timeout=300) as resp:
    data = resp.read()
digest = asset.get("digest") or ""
got = "sha256:" + hashlib.sha256(data).hexdigest()
if digest and digest != got:
    raise SystemExit(f"appimagetool digest {got} != {digest}")
with open(dest, "wb") as f:
    f.write(data)
os.chmod(dest, os.stat(dest).st_mode | stat.S_IEXEC)
PY

ARCH=x86_64 "$tool" --appimage-extract-and-run "$appdir" "$output"
