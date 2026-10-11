import test from 'node:test';
import assert from 'node:assert/strict';
import { updateCookies } from '../dist/service-cookies.js';

test('combined headers preserve owner identity after an Expires comma', () => {
  const jar = new Map();
  const headers = new Headers({ 'set-cookie': 'acw_tc=edge; Expires=Wed, 21 Oct 2030 07:28:00 GMT; Path=/, anonymous_id=owner; HttpOnly; Path=/, openmaic_cache_epoch=epoch; Path=/' });
  updateCookies(jar, headers);
  assert.deepEqual([...jar], [['acw_tc', 'edge'], ['anonymous_id', 'owner'], ['openmaic_cache_epoch', 'epoch']]);
  updateCookies(jar, new Headers({ 'set-cookie': 'acw_tc=new; Path=/' }));
  assert.equal(jar.get('anonymous_id'), 'owner');
});

test('native separate headers support replacement and removal', () => {
  const jar = new Map([['anonymous_id', 'old']]);
  updateCookies(jar, { getSetCookie: () => ['anonymous_id=new; Path=/', 'acw_tc=edge; Path=/'] });
  assert.equal(jar.get('anonymous_id'), 'new');
  updateCookies(jar, { getSetCookie: () => ['anonymous_id=gone; Max-Age=0; Path=/'] });
  assert.equal(jar.has('anonymous_id'), false);
});

test('revoked cookies are removed and Max-Age overrides Expires', () => {
  const jar = new Map([['anonymous_id', 'old'], ['acw_tc', 'old']]);
  updateCookies(jar, new Headers({ 'set-cookie': 'anonymous_id=revoked; Max-Age=-1, acw_tc=revoked; Expires=Wed, 21 Oct 2015 07:28:00 GMT' }));
  assert.equal(jar.size, 0);
  updateCookies(jar, new Headers({ 'set-cookie': 'anonymous_id=active; Max-Age=3600; Expires=Wed, 21 Oct 2015 07:28:00 GMT' }));
  assert.equal(jar.get('anonymous_id'), 'active');
});
