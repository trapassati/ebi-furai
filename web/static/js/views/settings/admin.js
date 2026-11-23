// Users, for administrators.

import { t } from '../../i18n.js';
import { del, get, post } from '../../api.js';
import { state } from '../../state.js';
import { actionSheet, clear, confirmDialog, formDialog, h, icon, showError, tile, toast } from '../../ui.js';

export async function loadUsers(box) {
  try {
    const r = await get('/api/admin/users');
    clear(box);
    r.users.forEach((u) => {
      const badges = [];
      if (u.is_admin) badges.push('admin');
      if (u.disabled) badges.push(t('disabled'));
      box.append(h('button', { class: 'row-btn px-3 py-2', onclick: () => userMenu(u, box) },
        tile('o-users', u.disabled ? 'plain' : (u.is_admin ? 'aurora' : 'sky'), 'sm'),
        h('div', { class: 'flex-1 min-w-0' },
          h('div', { class: 'text-[1.0625rem] truncate ' + (u.disabled ? 'text-muted line-through' : ''), text: u.username }),
          h('div', { class: 'text-[0.75rem] text-muted', text: [t('{0} feeds', u.feed_count || 0), ...badges].join(', ') })),
        icon('o-ellipsis-horizontal', 'w-5 h-5 text-muted')));
    });
  } catch (e) { showError(e); }
}

export function userMenu(u, box) {
  const self = u.id === state.me.user.id;
  actionSheet({
    title: u.username,
    actions: [
      {
        label: t('Reset password'), icon: 'o-key', onSelect: async () => {
          await formDialog({
            title: t('New password'), message: t('For {0}. Their sessions will be closed.', u.username), okLabel: t('Save'),
            fields: [{ name: 'password', type: 'password', placeholder: t('At least 8 characters'), autocomplete: 'new-password' }],
            validate: async (v) => {
              try { await post(`/api/admin/users/${u.id}/password`, { password: v.password }); } catch (e) { return e.message; }
              toast(t('Password reset'));
              return null;
            },
          });
        },
      },
      {
        // For a user who lost the phone and the recovery codes.
        label: t('Remove two-step verification'), icon: 'o-shield-check', onSelect: async () => {
          const ok = await confirmDialog({
            title: t('Remove the verification?'), message: t('{0} will sign in with the password alone and can turn it back on from the settings.', u.username),
            okLabel: t('Remove|2fa'), destructive: true, icon: 'o-shield-check',
          });
          if (!ok) return;
          try { await post(`/api/admin/users/${u.id}/totp/disable`); toast(t('Verification removed')); } catch (e) { showError(e); }
        },
      },
      {
        label: u.disabled ? t('Enable') : t('Disable'), icon: u.disabled ? 'o-check-circle' : 'o-x-mark', disabled: self,
        onSelect: async () => {
          try { await post(`/api/admin/users/${u.id}/${u.disabled ? 'enable' : 'disable'}`); loadUsers(box); } catch (e) { showError(e); }
        },
      },
      {
        label: t('Delete user'), icon: 'o-trash', destructive: true, disabled: self, onSelect: async () => {
          const ok = await confirmDialog({
            title: t('Delete {0}?', u.username), message: t('The user\'s feeds, folders and articles will be deleted.'),
            okLabel: t('Delete'), destructive: true,
          });
          if (!ok) return;
          try { await del(`/api/admin/users/${u.id}`); loadUsers(box); toast(t('User deleted')); } catch (e) { showError(e); }
        },
      },
    ],
  });
}

export async function createUser(box) {
  const r = await formDialog({
    title: t('New user'), okLabel: t('Create'),
    fields: [
      { name: 'username', placeholder: t('Username'), autocomplete: 'off' },
      { name: 'password', type: 'password', placeholder: t('Password (min. 8 characters)'), autocomplete: 'new-password' },
      { name: 'is_admin', type: 'switch', label: t('Administrator') },
    ],
    validate: async (v) => {
      try { await post('/api/admin/users', { username: v.username.trim(), password: v.password, is_admin: v.is_admin }); } catch (e) { return e.message; }
      return null;
    },
  });
  if (r) {
    toast(t('User created'));
    loadUsers(box);
  }
}
