# Changelog

All notable changes are listed here. Versions follow [Semantic Versioning](https://semver.org/).

## 1.10.0

- **YouTube channels as feeds**: the address of a channel (`/channel/UC…`, `@name`, `/c/…`, `/user/…`), of a
  playlist or of a video becomes the channel's (or playlist's) feed, without the cookie page YouTube shows from
  the EU. New videos arrive within seconds (YouTube's feeds use a WebSub hub).
- **Videos read well**: the thumbnail, large in the list with a play sign ("Shorts" on the short ones), and the
  description with its links as the article, in Reeder too. In the app the video plays inside the article: the
  player is loaded from youtube-nocookie.com only when you tap it. "Watch on YouTube" opens it in the YouTube app.
- **Hide Shorts** (feed settings of a YouTube channel): the Shorts no longer show in the lists, in the counters or in
  Reeder, the ones already there too; turned off, they come back as read.

## 1.9.0

- **Real time page for administrators** (Settings › Administration): whether instant updates work, the public
  address the hubs call and where it comes from (learned from the app or `PUBLIC_URL`), how many feeds get instant
  updates, wait for their hub or have errors, the hubs in use, the last notification received, and what a hub
  answered when it refused or never confirmed a subscription, in plain words, with a button to ask the hubs again
  now. Feeds of other users are not named.
- **Opened articles are counted per feed** (from this version on, nothing to do): the groundwork for the feed
  statistics, which will tell the feeds you read from those you only mark as read. Marking all as read does not
  count; articles opened offline count when they are sent to the server.

## 1.8.0

- **Settings in pages**: on the iPhone (and an iPad held upright or in Split View) the settings open on a list of
  sections, like the Settings app of iOS; a section slides in from the right, and "‹ Settings" or a swipe from the
  left edge goes back. An iPad held sideways shows the list on the left and one section on the right, like the
  computer.
- **Sections regrouped**: the account on top (password, two-step verification, devices, sign out); Appearance,
  Sounds (now on their own), Reading (with offline reading) and Notifications; Feeds and Apps and integrations
  (Reeder, Save for later, Add to Home Screen); for administrators, Administration with Users, Backup and the ad
  filter; About.
- **iPad**: the settings window no longer reaches up under the status bar; it keeps a margin at the top and bottom.

## 1.7.3

- **"You've read everything" shows the cards of the login page**, floating the same way, in place of the koi:
  as big as the round picture of the reader column next to it, and centred on the same line.

## 1.7.2

- **No more "Add as a preferred source on Google" badges** at the end of articles (9to5Mac and other news sites),
  also in articles already downloaded, with or without the ad filter.

## 1.7.1

- **Search finds words of the full text**: when an article's full text is downloaded (by hand or by the feed's
  option), the search looks into it, not only into the feed's summary.
- **"Read later" is safe from Reeder**: unsubscribing from it in a Google Reader app no longer deletes every saved
  page; it stays where it is.
- **Feeds that need a password**: a feed answering HTTP 401 now says to set the username and password in the feed
  menu (Sign-in with password…), or that the ones set were not accepted.
- **Articles without a title** are called "(senza titolo)" for people using the app in Italian, also the ones
  already downloaded.

## 1.7.0

- **Instant updates (WebSub)**: feeds whose site uses a WebSub hub get new articles within seconds of their
  publication instead of at the next refresh. Nothing to set up: the server learns its public address when an
  administrator uses the app from an `https://` address on the internet (`PUBLIC_URL` sets it by hand,
  `PUBLIC_URL=off` turns the feature off). The feed settings say where it is on. One subscription per feed address, however many people follow
  it, renewed before it ends and cancelled when nobody follows the feed any more; feeds with a password are left
  out. Notifications must be signed with the subscription's own secret, and only make the server download the
  feed as usual; those feeds are still checked every three hours as a safety net.

## 1.6.1

A general review of the code, server and app.

- Counters no longer flash back to old figures for a moment after "mark all as read" (an answer already on its
  way was applied after the change).
- Signing in as another person after a session expired starts the app afresh.
- The server stops its background work (backups, notifications, the ad lists, the update check) before closing
  the database, at shutdown and after a restore.
- Read later: the address is found anywhere in what the share sheet sends; apps syncing through the Google Reader
  API see a real address for the Read later feed.
- Messages that were only in Italian in the code are now in English and translated (8 notification times at most,
  a response too large).
- The example in the filters' word field is in the language of the app.
- Tests: the race detector and a check for known vulnerabilities run on every change; unused styles removed.
- The settings code is split into a file per section (no visible change).

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
