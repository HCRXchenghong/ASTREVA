import assert from 'node:assert/strict';
import test from 'node:test';
import { anonymousVisitorId, dailyAnonymousSalt, truncateIpForAnonymization } from './analytics-router.mjs';

test('匿名 UV：IP 先归并到 /24（IPv4）或 /64（IPv6），再参与 HMAC', () => {
  assert.equal(truncateIpForAnonymization('198.51.100.10'), '198.51.100.0');
  assert.equal(truncateIpForAnonymization('198.51.100.10:51234'), '198.51.100.0');
  assert.equal(truncateIpForAnonymization('::ffff:198.51.100.7'), '198.51.100.0');
  assert.equal(truncateIpForAnonymization('2001:db8:1:2:3:4:5:6'), '2001:db8:1:2');
  assert.equal(truncateIpForAnonymization('2001:db8:1:2::9'), '2001:db8:1:2');
  assert.equal(truncateIpForAnonymization('[2001:db8:1:2::9]:443'), '2001:db8:1:2');
  assert.equal(truncateIpForAnonymization('fe80::1%en0'), 'fe80:0:0:0');
  assert.equal(truncateIpForAnonymization(''), 'unknown');
});

test('匿名 UV：同 IP 同日稳定，跨天与跨网段不可关联', () => {
  const options = { day: '2026-10-19', secret: 'unit-test-secret' };
  const sameDay = anonymousVisitorId('198.51.100.10', options);
  const sameSubnet = anonymousVisitorId('198.51.100.11', options);
  assert.match(sameDay, /^anon_[0-9a-f]{32}$/);
  // 同一 /24 归并为同一个匿名键：当日去重更宽松，符合最小化原则。
  assert.equal(sameSubnet, sameDay);
  // 不同网段、不同日期都不可关联。
  assert.notEqual(anonymousVisitorId('203.0.113.10', options), sameDay);
  assert.notEqual(anonymousVisitorId('198.51.100.10', { ...options, day: '2026-10-20' }), sameDay);
  // 换根密钥即换整批匿名键。
  assert.notEqual(anonymousVisitorId('198.51.100.10', { ...options, secret: 'other-secret' }), sameDay);
});

test('匿名 UV：每日盐按天轮换且不泄露根密钥', () => {
  const first = dailyAnonymousSalt('2026-10-19', { secret: 'root' });
  const second = dailyAnonymousSalt('2026-10-20', { secret: 'root' });
  assert.equal(first.length, 32);
  assert.notDeepEqual(first, second);
  // 同一天的盐稳定，保证同一天内同一 IP 的 UV 去重可复现。
  assert.deepEqual(dailyAnonymousSalt('2026-10-19', { secret: 'root' }), first);
  assert.notDeepEqual(dailyAnonymousSalt('2026-10-19', { secret: 'another-root' }), first);
});
