import client from './client';

export interface SecuritySettings {
  preview_ttl_minutes: number;
}

export const fetchSecuritySettings = () =>
  client.get('/site-admin-api/security-settings').then((r) => r.data?.data ?? r.data);

export const updateSecuritySettings = (payload: { preview_ttl_minutes: number }) =>
  client.patch('/site-admin-api/security-settings', payload).then((r) => r.data?.data ?? r.data);
