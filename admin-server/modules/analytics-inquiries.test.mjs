import assert from 'node:assert/strict';
import test from 'node:test';
import { createEngineDashboardClient } from './analytics-router.mjs';

/**
 * 看板「询盘」口径回归：
 * 1. 引擎提供只读聚合接口时，直接用 today.inquiries / month.inquiries；
 * 2. 引擎尚未升级（404）时，退回管理员接口按会话聚合；
 * 3. 只有访客消息才算询盘，agent/AI 消息不算，同一会话多条消息只计 1 次；
 * 4. 引擎不可用时返回 ok:false，调用方应显示「未知」而不是 0。
 */

const RANGE = {
  todayStart: '2026-10-18T16:00:00.000Z',
  monthStart: '2026-09-30T16:00:00.000Z',
  monthEnd: '2026-10-31T16:00:00.000Z'
};

function jsonResponse(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' }
  });
}

test('询盘优先使用引擎只读聚合接口', async () => {
  const calls = [];
  const client = createEngineDashboardClient({
    apiUrl: 'http://engine.test',
    internalToken: 'internal-token',
    fetchImpl: async (url, options) => {
      calls.push({ url: String(url), headers: options?.headers || {} });
      return jsonResponse({
        today: { inquiries: 2 },
        month: { inquiries: 5 },
        definition: '测试口径'
      });
    }
  });

  const result = await client.read({ tz: 'Asia/Shanghai', month: '2026-10', range: RANGE });
  assert.equal(result.ok, true);
  assert.equal(result.today, 2);
  assert.equal(result.month, 5);
  assert.equal(result.source, 'engine-aggregate');
  assert.equal(result.definition, '测试口径');
  assert.match(calls[0].url, /\/api\/internal\/analytics\/dashboard\?/);
  assert.equal(calls[0].headers['X-Internal-Token'], 'internal-token');
});

test('引擎未升级时退回管理员接口：有访客消息的会话才计询盘', async () => {
  const conversations = [
    { id: 'conv_with_visitor', updated_at: '2026-10-19T06:00:00.000Z' },
    { id: 'conv_agent_only', updated_at: '2026-10-19T06:00:00.000Z' },
    // 本月没有更新过的会话不会被拉取消息，也不可能贡献本月询盘。
    { id: 'conv_old', updated_at: '2026-09-01T00:00:00.000Z' }
  ];
  const client = createEngineDashboardClient({
    apiUrl: 'http://engine.test',
    internalToken: 'internal-token',
    fetchImpl: async (url) => {
      const target = String(url);
      if (target.includes('/api/internal/analytics/dashboard')) return jsonResponse({}, 404);
      if (target.includes('/api/admin/login')) return jsonResponse({ token: 'admin-token' });
      if (target.includes('/api/admin/conversations')) return jsonResponse({ conversations });
      if (target.includes('/api/conversations/conv_with_visitor/messages')) {
        return jsonResponse({
          messages: [
            { sender_type: 'visitor', created_at: '2026-10-19T06:00:00.000Z' },
            // 同一会话第二条访客消息：只计 1 次。
            { sender_type: 'visitor', created_at: '2026-10-19T06:05:00.000Z' }
          ],
          has_more: false
        });
      }
      if (target.includes('/api/conversations/conv_agent_only/messages')) {
        return jsonResponse({
          messages: [
            { sender_type: 'agent', created_at: '2026-10-19T06:00:00.000Z' },
            { sender_type: 'ai', created_at: '2026-10-19T06:01:00.000Z' }
          ],
          has_more: false
        });
      }
      return jsonResponse({}, 404);
    }
  });

  const result = await client.read({ tz: 'Asia/Shanghai', month: '2026-10', range: RANGE });
  assert.equal(result.ok, true);
  assert.equal(result.source, 'admin-api');
  assert.equal(result.today, 1, '只有 conv_with_visitor 计入今日询盘');
  assert.equal(result.month, 1, '只有 conv_with_visitor 计入本月询盘');
});

test('引擎完全不可用时返回 ok:false，不得用 0 冒充无询盘', async () => {
  const client = createEngineDashboardClient({
    apiUrl: 'http://engine.test',
    internalToken: '',
    fetchImpl: async () => {
      throw new Error('connect ECONNREFUSED');
    }
  });
  const result = await client.read({ tz: 'Asia/Shanghai', month: '2026-10', range: RANGE });
  assert.equal(result.ok, false);
  assert.match(result.message, /不可用/);
});
