# Changelog

All notable changes are listed here. Versions follow [Semantic Versioning](https://semver.org/).

## 1.2.0

- **Reeder, NetNewsWire and other apps**: Ebi Furai now offers the Google Reader API (as FreshRSS does), so native
  apps can show your feeds and sync read and starred articles, folders and subscriptions. Apps sign in with
  **app passwords** (Settings → Reeder and other apps): one per app, shown once, working with two-step
  verification on, revocable one by one, with when and from where each was last used.
- **Update notice**: the server looks for a new release on GitHub twice a day; administrators see it when they
  open the app, with the release notes and the command for their installation (Docker or Linux service).
  `UPDATE_CHECK=false` turns it off.
- Fixed: "now" in the list of connected devices was always in Italian.

## 1.1.0

- **Restore a backup from the app**: Settings → Backup has a restore button next to every backup, and
  "Restore from a file…" for one saved elsewhere. The file is checked first (a sound Ebi Furai database, not
  from a newer version, with an administrator), the current state is saved as a backup, then the server
  restarts with the restored database and every device signs in again.
- **Image viewer**: tap a picture in an article to open it full screen; pinch or double tap to zoom, drag to
  move, swipe down to close. On the desktop: click, Ctrl + wheel or a trackpad pinch, Esc.
- The app no longer zooms like a web page: pinch and double-tap zoom are off everywhere else.
- Fixed: making a backup by hand twice in the same second wrote over the first one.

## 1.0.0

First public release.

- Feeds, folders and the four collections (unread, today, starred, all); OPML import and export.
- Reader with full text, appearance settings per device, reading time.
- Offline reading with a queue of actions; Web Push summaries and the app badge.
- Keyword filters, merged duplicates, per-feed settings (short name, always full text, always open on the site),
  "mark the ones above / below as read", undo for bulk actions.
- Feed health page, automatic daily backups, two-step verification, connected devices.
- iPhone, iPad, iPhone Duo and desktop layouts; keyboard shortcuts.
- English and Italian interface.
- Docker image for amd64 and arm64, Linux binaries with a systemd unit and an installer, first-run setup page.
