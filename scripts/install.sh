#!/bin/sh
# Installs or updates Ebi Furai as a systemd service, from the latest
# release on GitHub (Linux, amd64 or arm64). Run as root:
#
#   curl -fsSL https://raw.githubusercontent.com/trapassati/ebi-furai/main/scripts/install.sh | sudo sh
#
# Data:      /var/lib/ebi-furai   (database and backups)
# Settings:  /etc/ebi-furai/ebi-furai.env
# Service:   systemctl status ebi-furai / journalctl -u ebi-furai
set -eu
REPO=trapassati/ebi-furai
VERSION=${VERSION:-latest}

[ "$(id -u)" = 0 ] || { echo "Run as root (sudo)." >&2; exit 1; }
case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *) echo "Unsupported CPU: $(uname -m). Use the Docker image." >&2; exit 1 ;;
esac
command -v systemctl >/dev/null || { echo "systemd is needed." >&2; exit 1; }

if [ "$VERSION" = latest ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi
file="ebi-furai_linux_$ARCH.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fetch() { if command -v curl >/dev/null; then curl -fsSL "$1" -o "$2"; else wget -qO "$2" "$1"; fi; }
echo "Downloading $file ($VERSION)…"
fetch "$base/$file" "$tmp/$file"
fetch "$base/checksums.txt" "$tmp/checksums.txt"
(cd "$tmp" && grep " $file\$" checksums.txt | sha256sum -c -)
tar -xzf "$tmp/$file" -C "$tmp"

id ebi-furai >/dev/null 2>&1 || useradd --system --home-dir /var/lib/ebi-furai --shell /usr/sbin/nologin ebi-furai
install -m 0755 "$tmp/ebi-furai" /usr/local/bin/ebi-furai
install -d -m 0755 /etc/ebi-furai /usr/local/share/doc/ebi-furai
[ -f /etc/ebi-furai/ebi-furai.env ] || install -m 0640 -g ebi-furai "$tmp/.env.example" /etc/ebi-furai/ebi-furai.env
cp -r "$tmp/LICENSE" "$tmp/NOTICE" "$tmp/licenses" /usr/local/share/doc/ebi-furai/ 2>/dev/null || true
install -m 0644 "$tmp/ebi-furai.service" /etc/systemd/system/ebi-furai.service
systemctl daemon-reload
if systemctl is-active --quiet ebi-furai; then
  systemctl restart ebi-furai
  echo "Ebi Furai updated and restarted."
else
  systemctl enable --now ebi-furai
  echo "Ebi Furai is running on port 9099."
  echo "Open http://$(hostname -I 2>/dev/null | awk '{print $1}'):9099 and use the setup code from:"
  echo "  journalctl -u ebi-furai | grep 'setup code'"
fi
