// "Informazioni": version, source code (AGPL-3.0, section 13), license,
// third-party components, icon credits and privacy.

import { t } from '../i18n.js';
import { get } from '../api.js';
import { state } from '../state.js';
import { h, icon, openSheet, showError, tile } from '../ui.js';

// Components shipped with the app. Keep the versions in step with the files
// in web/static/vendor, the sprite and the build (README, "Versioni").
const APP_PARTS = [
  { name: 'GSAP', version: '3.15.1', license: 'GSAP Standard "No Charge" License', by: 'GreenSock / Webflow', url: 'https://gsap.com/community/standard-license/' },
  { name: 'Lenis', version: '1.3.27', license: 'MIT', by: 'darkroom.engineering', url: 'https://github.com/darkroomengineering/lenis' },
  { name: 'Heroicons', version: '2.2.0', license: 'MIT', by: 'Tailwind Labs', url: 'https://github.com/tailwindlabs/heroicons' },
  { name: 'Outfit', version: '5.3.0', license: 'SIL Open Font License 1.1', by: 'The Outfit Project Authors', url: 'https://github.com/Outfitio/Outfit-Fonts' },
  { name: 'M PLUS Rounded 1c', version: t('1.060, only for エビフライ'), license: 'SIL Open Font License 1.1', by: 'The Rounded M+ Project Authors', url: 'https://github.com/coz-m/MPLUS_FONTS' },
  { name: 'uqr', version: '0.1.3', license: 'MIT', by: 'Anthony Fu, Project Nayuki', url: 'https://github.com/unjs/uqr' },
  { name: 'Tailwind CSS', version: t('4.3.5, only for the build'), license: 'MIT', by: 'Tailwind Labs', url: 'https://github.com/tailwindlabs/tailwindcss' },
];

const FILTER_LISTS = [
  { name: 'EasyList', license: t('GPL-3.0 or CC BY-SA 3.0'), url: 'https://easylist.to/' },
  { name: 'EasyList Italia', license: t('GPL-3.0 or CC BY-SA 3.0'), url: 'https://easylist.to/' },
];

function section(label, ...children) {
  return h('div', {}, h('div', { class: 'group-label', text: label }), ...children);
}

// A row that opens an outside page.
function linkRow(tileIcon, tone, title, subtitle, url) {
  return h('a', { class: 'row-btn px-3 py-2.5', href: url, target: '_blank', rel: 'noopener noreferrer' },
    tileIcon ? tile(tileIcon, tone, 'sm') : null,
    h('span', { class: 'flex-1 min-w-0' },
      h('span', { class: 'block text-[1rem] truncate', text: title }),
      subtitle ? h('span', { class: 'block text-[0.8125rem] text-muted', text: subtitle }) : null),
    icon('o-arrow-top-right-on-square', 'w-4 h-4 text-faint flex-none'));
}

function para(text, cls = '') {
  return h('p', { class: 'text-[0.9375rem] leading-relaxed ' + cls, text });
}

function sheetHeader(title, sheetRef, backLabel) {
  return h('div', { class: 'flex items-center px-2 h-14 sticky top-0 bg-bg z-10' },
    h('button', { class: 'icon-btn on w-28 justify-start font-medium text-[1.0625rem]', onclick: () => sheetRef.sheet.close() },
      icon('o-chevron-left', 'w-5 h-5'), backLabel),
    h('div', { class: 'flex-1 text-center sheet-title truncate', text: title }),
    h('div', { class: 'w-28' }));
}

export function openAbout() {
  const me = state.me;
  const v = encodeURIComponent(me.version || '');
  const ref = {};
  const versionLine = h('div', { class: 'text-[0.8125rem] text-muted', text: t('Version {0}', me.version) });
  const sourceBox = h('div', { class: 'group' }, h('div', { class: 'px-4 py-3 text-muted text-[0.9375rem]', text: t('Loading…') }));
  const serverBox = h('div', { class: 'group' }, h('div', { class: 'px-4 py-3 text-muted text-[0.9375rem]', text: t('Loading…') }));

  const content = h('div', { class: 'px-4 pb-10' },
    h('div', { class: 'about-hero' },
      h('img', { class: 'about-logo', src: `/static/icons/shrimp.png?v=${v}`, alt: '' }),
      h('div', { class: 'about-name', text: 'エビフライ' }),
      h('div', { class: 'text-[1.0625rem] font-semibold', text: me.app_name || 'Ebi Furai' }),
      h('div', { class: 'text-[0.9375rem] text-muted', text: t('RSS reader to install on your own server') }),
      versionLine),

    section(t('Source code'), sourceBox),

    section(t('License'), h('div', { class: 'group px-4 py-4 flex flex-col gap-3' },
      para(t('Ebi Furai is free software, released under the GNU Affero General Public License, version 3 (AGPL-3.0).')),
      para(t('You may use, study, modify and redistribute it, also for commercial purposes. If you modify it and make it available to others, even only as an online service, you must publish the code of your changes under the same license.')),
      para(t('As an exception allowed by the license itself, it may be used together with GSAP, the free but not open source animation library the app uses.')),
      para(t('It comes as is, without any warranty.'), 'text-muted')),
    h('div', { class: 'group mt-3' },
      h('button', { class: 'row-btn px-3 py-2.5', onclick: () => showText('GNU AGPL v3', '/static/legal/AGPL-3.0.txt?v=' + v) },
        tile('o-scale', 'indigo', 'sm'), h('span', { class: 'flex-1 text-[1rem]', text: t('Read the full text') }),
        icon('o-chevron-right', 'w-4 h-4 text-faint')),
      h('button', { class: 'row-btn px-3 py-2.5', onclick: () => showText(t('Copyright and exception'), '/static/legal/NOTICE.txt?v=' + v) },
        tile('o-document-text', 'sky', 'sm'), h('span', { class: 'flex-1 text-[1rem]', text: t('Copyright and GSAP exception') }),
        icon('o-chevron-right', 'w-4 h-4 text-faint')))),

    section(t('App components'), h('div', { class: 'group' },
      ...APP_PARTS.map((p) => linkRow(null, '', p.name, `${p.version}, ${p.license}, ${p.by}`, p.url)))),

    section(t('Server components'), serverBox),

    section(t('Ad filter'), h('div', { class: 'group' },
      ...FILTER_LISTS.map((l) => linkRow(null, '', l.name, t('{0}; downloaded by the server, not included', l.license), l.url)))),

    section('Privacy', h('div', { class: 'group px-4 py-4 flex flex-col gap-3' },
      para(t('All data (feeds, articles, accounts, preferences) stays on the server where Ebi Furai is installed. There are no statistics, no tracking and no third-party services.')),
      para(t('The server only connects to the outside to download feeds and full articles, article images (which go through the server, so sites don\'t see your device), site icons and the ad filter lists. Notifications go through Apple\'s or the browser\'s push service.')))),

    h('p', { class: 'text-center text-[0.8125rem] text-muted pt-8', text: '© 2026 Fartingloud' }));

  ref.sheet = openSheet(h('div', { class: 'min-h-full' }, sheetHeader(t('About'), ref, t('Back')), content), { full: true, wide: true });
  loadServer(sourceBox, serverBox, versionLine);
}

async function loadServer(sourceBox, serverBox, versionLine) {
  let info;
  try {
    info = await get('/api/about');
  } catch (e) {
    sourceBox.replaceChildren(h('div', { class: 'px-4 py-3 text-muted text-[0.9375rem]', text: t('Not available offline.') }));
    serverBox.replaceChildren(h('div', { class: 'px-4 py-3 text-muted text-[0.9375rem]', text: t('Not available offline.') }));
    if (e.status) showError(e);
    return;
  }
  const ver = info.release && info.release !== 'dev' ? `${info.release} (${info.version})` : info.version;
  versionLine.textContent = t('Version {0}, built with {1}', ver, info.go.replace(/^go/, 'Go '));
  if (info.source_url) {
    sourceBox.replaceChildren(linkRow('o-code-bracket', 'aurora', t('Open the source code'), info.source_url.replace(/^https?:\/\//, ''), info.source_url));
  } else {
    sourceBox.replaceChildren(h('div', { class: 'px-4 py-3 flex flex-col gap-1' },
      h('div', { class: 'text-[1rem]', text: t('Address not set yet') }),
      h('div', { class: 'text-[0.8125rem] text-muted', text: state.me.user.is_admin
        ? t('Set where the code is published with SOURCE_URL in the .env file: the AGPL license asks to offer it to those who use the app.')
        : t('Ask the administrator where the code is published.') })));
  }
  // The Go license asks binaries to carry its text: it is shown here.
  const v = encodeURIComponent(state.me.version || '');
  const rows = [h('button', { class: 'row-btn px-3 py-2.5', onclick: () => showText(t('Go license'), '/static/legal/Go-LICENSE.txt?v=' + v, 'https://go.dev/LICENSE') },
    h('span', { class: 'flex-1 min-w-0' },
      h('span', { class: 'block text-[1rem]', text: 'Go' }),
      h('span', { class: 'block text-[0.8125rem] text-muted', text: t('{0}, BSD-3-Clause, The Go Authors. Tap for the text', info.go.replace(/^go/, '')) })),
    icon('o-chevron-right', 'w-4 h-4 text-faint flex-none'))];
  for (const m of info.modules) rows.push(linkRow(null, '', m.path, `${m.version}${m.license ? ', ' + m.license : ''}`, m.url));
  serverBox.replaceChildren(...rows);
}

// A license text, on its own sheet over the page.
async function showText(title, url, online = 'https://www.gnu.org/licenses/agpl-3.0.html') {
  const ref = {};
  const box = h('div', { class: 'legal-text', lang: 'en' }, para(t('Loading…'), 'text-muted'));
  ref.sheet = openSheet(h('div', { class: 'min-h-full' }, sheetHeader(title, ref, t('Back')), h('div', { class: 'px-5 pb-10' }, box)), { full: true, wide: true });
  try {
    const r = await fetch(url);
    if (!r.ok) throw new Error();
    box.replaceChildren(...reflow(await r.text()));
  } catch {
    box.replaceChildren(para(t('Text not available offline. You can also find it at {0}', online), 'text-muted'));
  }
}

// The license file is wrapped at 80 columns for terminals; on a phone the
// lines of each paragraph are joined again so the text wraps to the screen.
// Short paragraphs without a final period are the headings.
// A line starting with "*" opens a new item (the BSD conditions).
function reflow(text) {
  return text.split(/\n\s*\n/).flatMap((p) => p.trim().split(/\n(?=\s*\*\s)/))
    .map((p) => p.trim().split('\n').map((l) => l.trim()).join(' ')).filter(Boolean)
    .map((p) => (p.length < 70 && !/[.;:,]$/.test(p)
      ? h('h3', { class: 'legal-head', text: p })
      : h('p', { text: p })));
}
