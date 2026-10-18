import assert from 'node:assert/strict';
import test from 'node:test';
import { checkCmsContentContract } from '../../scripts/check-cms-content-contract.mjs';

test('前端 SiteContent 必填字段全部存在于后台内容种子', async () => {
  const result = await checkCmsContentContract();
  assert.deepEqual(result.missing, []);
  assert.deepEqual(result.invalid, []);
  assert.deepEqual(result.hardcoded, []);
});
