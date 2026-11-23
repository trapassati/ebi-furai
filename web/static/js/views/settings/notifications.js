// Notifications: this device, the summary of unread articles and its times.

import { locale, t } from '../../i18n.js';
import { del, post, put } from '../../api.js';
import { actionSheet, clear, confirmDialog, h, icon, picker, showError, tile, toast, toggleSwitch } from '../../ui.js';
import * as push from '../../push.js';

const HALF_HOURS = Array.from({ length: 48 }, (_, i) => `${String(Math.floor(i / 2)).padStart(2, '0')}:${i % 2 ? '30' : '00'}`);
const dayFmt = new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });

export function notifyControls() {
  const box = h('div', { class: 'flex flex-col gap-3' }, h('div', { class: 'group px-4 py-4 text-muted', text: t('Loading…') }));
  let info = null;

  const save = async () => {
    try { await put('/api/push/settings', info.settings); } catch (e) { showError(e); }
  };

  const render = () => {
    clear(box);
    const reason = push.unavailableReason();
    const st = info.settings;

    // This device
    const devSwitch = toggleSwitch(info.subscribed);
    devSwitch.disabled = !!reason;
    devSwitch.addEventListener('change', async () => {
      devSwitch.disabled = true;
      try {
        if (devSwitch.checked) {
          await push.enable(info.public_key); // first await: keeps the tap gesture for iOS
          toast(t('Notifications on for this device'));
        } else {
          await push.disable();
        }
      } catch (e) {
        showError(e);
      }
      info = await push.info().catch(() => info);
      render();
    });
    const top = h('div', { class: 'group px-4' },
      h('div', { class: 'switch-row' },
        h('div', {}, h('div', { class: 'text-[1rem]', text: t('Notifications on this device') }),
          reason ? null : h('div', { class: 'text-[0.75rem] text-muted', text: info.subscribed ? t('On') : t('Off') })),
        devSwitch));
    if (reason) top.append(h('div', { class: 'py-3 text-[0.8125rem] text-muted', text: reason }));
    box.append(top);

    // Schedule (shared by all devices of the user)
    const enabled = toggleSwitch(st.enabled);
    enabled.addEventListener('change', () => { st.enabled = enabled.checked; save(); render(); });
    const chips = h('div', { class: 'flex flex-wrap gap-2 py-3' });
    st.times.forEach((hm) => chips.append(h('button', {
      class: 'chip', 'aria-label': t('Remove {0}', hm),
      onclick: () => { st.times = st.times.filter((x) => x !== hm); save(); render(); },
    }, hm, icon('o-x-mark', 'w-4 h-4'))));
    if (st.times.length < 8) {
      chips.append(h('button', {
        class: 'chip chip-add',
        onclick: async () => {
          const choice = await actionSheet({
            title: t('Add a time'),
            actions: HALF_HOURS.filter((hm) => !st.times.includes(hm)).map((hm) => ({ label: hm, value: hm })),
          });
          if (!choice) return;
          st.times = [...st.times, choice.value].sort();
          save();
          render();
        },
      }, icon('o-plus', 'w-4 h-4'), t('Time')));
    }

    const quiet = toggleSwitch(st.quiet_enabled);
    quiet.addEventListener('change', () => { st.quiet_enabled = quiet.checked; save(); render(); });
    const qStart = picker({ title: t('Quiet hours start'), value: st.quiet_start, options: HALF_HOURS.map((hm) => ({ value: hm, label: hm })) });
    const qEnd = picker({ title: t('Quiet hours end'), value: st.quiet_end, options: HALF_HOURS.map((hm) => ({ value: hm, label: hm })) });
    qStart.addEventListener('change', () => { st.quiet_start = qStart.value; save(); });
    qEnd.addEventListener('change', () => { st.quiet_end = qEnd.value; save(); });

    const sched = h('div', { class: 'group px-4' },
      h('div', { class: 'switch-row' },
        h('div', {}, h('div', { class: 'text-[1rem]', text: t('Unread summary') }),
          h('div', { class: 'text-[0.75rem] text-muted', text: t('At the chosen times, if there are articles to read') })),
        enabled));
    if (st.enabled) {
      sched.append(h('div', {}, chips));
      sched.append(h('div', { class: 'switch-row' }, h('span', { class: 'text-[1rem]', text: t('Quiet hours') }), quiet));
      if (st.quiet_enabled) {
        sched.append(h('div', { class: 'flex items-center gap-3 py-3' },
          h('span', { class: 'text-muted text-[0.875rem]', text: t('From') }), qStart,
          h('span', { class: 'text-muted text-[0.875rem]', text: 'alle' }), qEnd));
      }
    }
    box.append(sched);

    // Devices
    if (info.devices.length) {
      const list = h('div', { class: 'group' });
      info.devices.forEach((d) => {
        let sub = t('Never received a notification');
        if (d.last_ok_at) sub = t('Last notification ') + dayFmt.format(new Date(d.last_ok_at * 1000));
        if (d.fail_count) sub = t('Delivery error: {0}', d.last_error);
        list.append(h('div', { class: 'row-btn px-3 py-2' },
          tile(d.current ? 'o-check-circle' : 'o-user-circle', d.current ? 'aurora' : 'plain', 'sm'),
          h('div', { class: 'flex-1 min-w-0' },
            h('div', { class: 'text-[1rem] truncate', text: d.device + (d.current ? t(' (this one)') : '') }),
            h('div', { class: 'text-[0.75rem] truncate ' + (d.fail_count ? 'text-danger' : 'text-muted'), text: sub })),
          h('button', {
            class: 'icon-btn', 'aria-label': t('Remove device'),
            onclick: async () => {
              const ok = await confirmDialog({ title: t('Remove the device?'), message: t('{0} will no longer receive notifications.', d.device), okLabel: t('Remove'), destructive: true, icon: 'o-device-phone-mobile' });
              if (!ok) return;
              try {
                if (d.current) await push.disable(); else await del(`/api/push/subscriptions/${d.id}`);
                info = await push.info();
                render();
              } catch (e) { showError(e); }
            },
          }, icon('o-trash', 'w-5 h-5'))));
      });
      box.append(h('div', { class: 'field-label pt-1', text: t('Devices receiving notifications') }), list);
      const testBtn = h('button', { class: 'btn btn-plain w-full' }, icon('o-sparkles', 'w-5 h-5'), t('Send a test notification'));
      testBtn.addEventListener('click', async () => {
        testBtn.disabled = true;
        try {
          const r = await post('/api/push/test');
          toast(r.failed ? t('Sent to {0} devices, {1} not reached', r.sent, r.failed) : (r.sent === 1 ? t('Sent to 1 device') : t('Sent to {0} devices', r.sent)));
          info = await push.info();
          render();
        } catch (e) { showError(e); }
        testBtn.disabled = false;
      });
      box.append(testBtn);
    }
    box.append(h('div', { class: 'px-1 text-[0.75rem] text-muted', text: t('Times in the {0} time zone. Each summary also updates the number on the app\'s icon.', info.timezone) }));
  };

  push.info().then((i) => { info = i; render(); }).catch((e) => {
    clear(box).append(h('div', { class: 'group px-4 py-4 text-muted', text: t('Notification settings are not available offline.') }));
    showError(e);
  });
  return box;
}
