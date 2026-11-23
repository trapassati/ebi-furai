// "Read later": pages saved from the app, from the browser with a
// bookmarklet, or from the iPhone's share sheet with a Shortcut. They have
// their own collection, "Read later", shown once there is one.

import { del, post } from '../api.js';
import { t } from '../i18n.js';
import { formDialog, h, icon, openSheet, showError, toast } from '../ui.js';
import { createAppPassword, valueRow } from './apps.js';

let ctx = null;

export function initSaved(context) {
  ctx = context;
}

const saveEndpoint = () => location.origin + '/api/save';

// The bookmarklet opens Ebi Furai in a small window at #/save?url=…; the
// app, signed in, saves the page and closes the window.
export function bookmarklet() {
  const target = JSON.stringify(location.origin + '/#/save?url=');
  return `javascript:(()=>{window.open(${target}+encodeURIComponent(location.href),'ebifurai-save','width=440,height=420')})()`;
}

// savePage stores a page and tells how it went. Returns the article or null.
export async function savePage(url) {
  let r;
  try {
    r = await post('/api/saved', { url });
  } catch (e) {
    showError(e);
    return null;
  }
  if (r.problem) toast(t('Saved, but the page could not be read: {0}', r.problem));
  else toast(r.again ? t('Back at the top of Read later') : t('Saved in Read later'));
  if (ctx) await ctx.reloadSources();
  return r.article;
}

// The "+" menu: an address to save, from the clipboard if there is one.
export async function saveDialog() {
  let saved = null;
  const ok = await formDialog({
    title: t('Save a page'),
    message: t('Its text is kept in Read later, to read when you like, offline too.'),
    okLabel: t('Save'),
    fields: [{ name: 'url', type: 'url', placeholder: 'https://…', inputmode: 'url' }],
    validate: async (v) => {
      const url = (v.url || '').trim();
      if (!/^https?:\/\/\S+$/i.test(url)) return t('Type the address of the page, starting with https://');
      saved = await savePage(url);
      return saved ? null : t('The page could not be saved.');
    },
  });
  if (ok && saved && ctx) ctx.showSaved();
}

// Opened by the bookmarklet (#/save?url=…).
export async function saveFromBookmarklet(url) {
  const popup = window.name === 'ebifurai-save';
  const a = await savePage(url);
  if (popup && a) {
    // Done: the little window closes by itself.
    setTimeout(() => window.close(), 1600);
  } else if (a && ctx) {
    ctx.showSaved();
  }
}

export async function removeSaved(id) {
  try {
    await del(`/api/saved/${id}`);
    toast(t('Removed from Read later'));
    return true;
  } catch (e) {
    showError(e);
    return false;
  }
}

// ---- how to save from elsewhere ----

function step(n, text, ...extra) {
  return h('div', { class: 'flex gap-3 px-4 py-3' },
    h('span', { class: 'save-step', text: String(n) }),
    h('div', { class: 'flex-1 min-w-0 flex flex-col gap-2' }, h('div', { class: 'text-[0.9375rem]', text }), ...extra));
}

export function openSaveHelp() {
  const ref = {};
  const mark = h('a', {
    class: 'bookmarklet', href: bookmarklet(), draggable: 'true',
    title: t('Drag it to the bookmarks bar'),
    onclick: (e) => { e.preventDefault(); toast(t('Drag it to the bookmarks bar of the browser')); },
  }, icon('o-bookmark', 'w-5 h-5'), t('Save to Ebi Furai'));

  const shortcutRows = (secret) => h('div', { class: 'group' },
    valueRow(t('Address (URL)'), saveEndpoint()),
    valueRow(t('Header Authorization'), secret ? 'Bearer ' + secret : 'Bearer …'));

  const makeKey = () => createAppPassword({
    name: t('Shortcut "Save to Ebi Furai"'),
    extra: {
      message: t('Put it in the Shortcut, after "Bearer ". You won\'t see it again: if you lose it, make a new one.'),
      rows: shortcutRows,
    },
  });

  const content = h('div', { class: 'px-4 pb-10 flex flex-col gap-3' },
    h('p', { class: 'text-[0.9375rem] text-muted px-1', text: t('Pages you save end up in Read later, with their text ready to read, offline too. Once read, they leave the collection like any article.') }),

    h('div', { class: 'group-label', text: t('In the app') }),
    h('div', { class: 'group' },
      h('button', { class: 'row-btn px-4 py-3 w-full text-left', onclick: () => { ref.sheet.close(); setTimeout(saveDialog, 300); } },
        icon('o-plus', 'w-5 h-5 text-accent flex-none'),
        h('span', { class: 'flex-1', text: t('Save a page from its address') }))),

    h('div', { class: 'group-label', text: t('On the computer') }),
    h('div', { class: 'group px-4 py-3 flex flex-col gap-3' },
      h('p', { class: 'text-[0.9375rem]', text: t('Drag this button to the bookmarks bar of the browser. On a page to read later, click it: the page is saved in a moment.') }),
      mark,
      h('p', { class: 'text-[0.8125rem] text-muted', text: t('It works in the browser where you are signed in to Ebi Furai.') })),

    h('div', { class: 'group-label', text: t('On the iPhone and the iPad') }),
    h('p', { class: 'text-[0.875rem] text-muted px-1', text: t('A Shortcut puts "Save to Ebi Furai" in the share button of Safari and of the other apps. It is made once, in the Shortcuts app:') }),
    h('div', { class: 'group' },
      step(1, t('Make an app password for the Shortcut.'),
        h('button', { class: 'btn btn-plain self-start !min-h-10', onclick: makeKey }, icon('o-key', 'w-5 h-5'), t('New app password'))),
      step(2, t('In Shortcuts, make a new shortcut. In its details (ⓘ) turn on "Show in Share Sheet", for URLs and Safari web pages.')),
      step(3, t('Add the action "Get Contents of URL" with this address, and set Method to POST:'), shortcutRows(null)),
      step(4, t('In Headers add Authorization, with "Bearer " followed by the app password. In Request Body choose JSON, add the field url and set it to "Shortcut Input".')),
      step(5, t('Name it "Save to Ebi Furai". From Safari or any app: Share → Save to Ebi Furai.'))),
    h('p', { class: 'text-[0.8125rem] text-muted px-1', text: t('In Safari on the iPhone the bookmarklet works too, if you are signed in to Ebi Furai in Safari.') }));
  const header = h('div', { class: 'flex items-center px-2 h-14 sticky top-0 bg-bg z-10' },
    h('div', { class: 'w-20' }),
    h('div', { class: 'flex-1 text-center sheet-title', text: t('Save for later') }),
    h('button', { class: 'icon-btn on w-20 font-semibold text-[1.0625rem]', onclick: () => ref.sheet.close(), text: t('Done') }));
  ref.sheet = openSheet(h('div', { class: 'min-h-full' }, header, content), { full: true, wide: true });
}
