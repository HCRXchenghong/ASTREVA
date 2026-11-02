import assert from 'node:assert/strict';
import test from 'node:test';
import { geolocationFromRequest } from './request-geo.mjs';

test('访问地理信息：未配置可信代理时忽略可伪造的地理请求头', () => {
  assert.deepEqual(
    geolocationFromRequest({
      headers: {
        'cf-ipcountry': 'US',
        'x-vercel-ip-city': 'San%20Francisco',
        'x-vercel-ip-timezone': 'America/Los_Angeles'
      }
    }),
    { country: '', region: '', city: '', timezone: '' }
  );
});

test('访问地理信息：从可信代理读取并校验国家、地区、城市与 IANA 时区', () => {
  assert.deepEqual(
    geolocationFromRequest(
      {
        headers: {
          'cf-ipcountry': 'us',
          'x-vercel-ip-country-region': 'CA',
          'x-vercel-ip-city': 'San%20Francisco',
          'x-vercel-ip-timezone': 'America/Los_Angeles'
        }
      },
      { trustedProxy: true }
    ),
    {
      country: 'US',
      region: 'CA',
      city: 'San Francisco',
      timezone: 'America/Los_Angeles'
    }
  );
});

test('访问地理信息：丢弃无效国家和时区，并清理控制字符', () => {
  assert.deepEqual(
    geolocationFromRequest(
      {
        headers: {
          'cf-ipcountry': 'Unknown',
          'cf-region': 'North\nDistrict',
          'cf-ipcity': 'Town',
          'cf-timezone': 'Not/A_Real_Zone'
        }
      },
      { trustedProxy: true }
    ),
    { country: '', region: 'NorthDistrict', city: 'Town', timezone: '' }
  );
});
