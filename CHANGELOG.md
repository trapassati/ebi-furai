# Changelog

All notable changes are listed here. Versions follow [Semantic Versioning](https://semver.org/).

## 1.6.0

- **Listen to the article**: the speaker button in the reader (or `L`) reads it aloud with the voice of the device,
  in the language of the article. A small player floats over it: back and forward by a paragraph, play and pause,
  the speed (0.8× to 1.5×, kept on the device). The paragraph being read is highlighted and kept in view; at the
  end the player closes. On the iPhone the voice stops when the screen locks, as in Safari.
- **Read later**: save any page to read it in Ebi Furai, in its own **Read later** collection (shown once
  something is saved), with the text extracted, offline too. From the app (+ → Save a page for later), from the
  browser with a bookmarklet, or from the iPhone's share button with a Shortcut that uses an app password (Settings
  → Save for later explains both). Saved pages stay out of Unread, Today, All and the badge; unread ones are never
  cleaned up; a page saved again comes back to the top. They show the site they come from, and can be removed from
  the list's menu.
- **Desktop: scrollbars only while scrolling**, fading out after a moment as on macOS.
- **Fixed: the Aa window in the reader** no longer fits only half on an iPhone: it holds what changes the article
  (theme, font, size, line spacing), with Done at the top. Language, list text, sounds and the rest are in
  Settings → Appearance.
- **Fixed: on the iPad in landscape** the button to hide the first column works again (broken in 1.4.1).

## 1.5.0

- **Sound effects**, off by default: Settings → Appearance → Sound effects, with a volume (low, medium, high).
  A soft bell when a refresh brings new articles, a crystal when you star one, a drop when you mark one as read,
  a swoosh for "mark all as read" and two low thumps when something fails. They are made in the app (no audio
  files), they belong to the device, and they never play for articles marked as read while scrolling or when
  opened. On the iPhone the silent switch mutes them and music from other apps keeps playing.
- **Backup copies on a WebDAV server** (Nextcloud, ownCloud, a NAS, Koofr…): Settings → Backup → Copy to a WebDAV
  server. The address, username and password are checked before saving (the folder is created if missing); the
  password is stored sealed like the feed passwords and never shown again. Every backup is copied there, with
  the same rotation as on the server; a failed copy is retried every hour and shown in the settings, and
  "Copy now" copies at once. Files in that folder that are not Ebi Furai backups are never touched.
- **Delete a backup** from the list (trash button). Its copy on WebDAV, if any, stays until the rotation removes
  it there.
- **Notification of a new version** to the administrators, on the devices where they turned notifications on:
  once per version, without touching the unread badge. Tapping it opens the release notes and the update
  command.

## 1.4.1

- **iPad: a narrower first column** (272 points instead of 320, in both orientations), as on the computer: the
  collections and the feeds are single rows with their count, 44 points tall for the finger, without the
  progress bar and the line under the name. The iPhone keeps its tiles.

## 1.4.0

- **Desktop in the style of the latest macOS** (Liquid Glass), in Ebi Furai's colors: the sidebar floats as a
  glass panel, toolbar buttons sit in glass capsules over the content, which scrolls softly under them;
  rounder buttons, fields and selections; menus, tooltips and windows of glass, very round.
- **The article list is made of cards again** on the desktop too, sized for a computer, with a highlight under
  the mouse; the compact rows of 1.3 are gone.

## 1.3.2

- Windows no longer open with their first button ("Done", "Back") outlined: the focus goes to the window itself,
  which draws no ring (Tab still starts from its top). Closing a menu after a click leaves no outlined button
  either; after the keyboard, the focus goes back where it was.

## 1.3.1

- Desktop: a window opened from the settings (About, keyboard shortcuts, connected devices…) was drawn under
  the header of the settings, which covered its "Done" and "Back" buttons. It now takes the place of the
  settings window, at the same size, and "Back" returns to it.
- Desktop: "You've read everything" sits on the same line as "No article open" again, and its hint says the
  list filter is at the top, to click.

## 1.3.0

- **A desktop interface on computers.** With a mouse or trackpad the app no longer looks like a phone app made
  bigger: commands sit in a thin bar at the top of each column instead of floating at the bottom; the article
  list is compact, with rows instead of cards; the sidebar lists the collections and feeds as in a mail app;
  menus (right click, "Add", order, pickers) open where you click and work with the arrow keys; dialogs have a
  measured size and a lighter shade behind; buttons are small and show what they do, with their shortcut,
  when hovered. The settings become a window with the sections on the left.
- **Resizable columns**: drag the edge of the sidebar or of the list; double click to go back to the standard
  width. Remembered on each computer.
- iPhone and iPad (touch) keep their interface as it was.

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
