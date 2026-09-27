import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

// Exercise the actual embedded script with browser doubles. These tests never
// invoke osascript, send Apple events, or read the user's browser tabs.
const source = readFileSync(new URL('../cmd/devjay/native_scripts/pr_browser.js', import.meta.url), 'utf8');
function fixture(specs) {
  const apps = new Map();
  const actions = [];
  for (const spec of specs) {
    const windows = (spec.windows || []).map((urls, index) => {
      const list = urls.map(url => ({url: () => url}));
      const window = {id: () => index + 1, tabs: () => list};
      window.tabs.push = tab => { actions.push(['tab', spec.id, index, tab.url()]); list.push(tab); };
      for (const property of ['currentTab', 'miniaturized', 'activeTabIndex', 'minimized', 'index']) {
        Object.defineProperty(window, property, {set(value) { actions.push([property, spec.id, index, value]); }});
      }
      return window;
    });
    const app = {
      running: () => spec.running !== false,
      windows: () => { if (spec.denied) throw Error('not allowed'); return windows; },
      Tab: ({url}) => ({url: () => url}),
      activate: () => { if (spec.failActivate) throw Error('activation failed'); actions.push(['activate', spec.id]); }
    };
    app.windows.byId = id => windows[id - 1];
    apps.set(spec.id, app);
  }
  const context = vm.createContext({Application: id => { assert.ok(apps.has(id)); return apps.get(id); }});
  vm.runInContext(source, context);
  return {actions, context, navigate: (url, newTab = false, defaultID = '') =>
    JSON.parse(JSON.stringify(context.navigatePR(url, newTab, specs, defaultID)))};
}

const url = 'https://github.com/team/project/pull/25';
test('recognizes PR pages without confusing other numbers, hosts, or repositories', () => {
  const {context} = fixture([]);
  for (const value of [url, `${url}/files`, `${url}/commits/abc?diff=split#top`, url.toUpperCase()]) {
    assert.equal(context.prIdentity(value), 'team/project/pull/25');
  }
  for (const value of [`${url}0`, url.replace('project', 'other')]) assert.notEqual(context.prIdentity(value), 'team/project/pull/25');
  for (const value of ['https://github.com.evil/team/project/pull/25', 'javascript:alert(1)', 'https://github.com/team/../pull/25', `${url}x`, 'https://github.com@evil/team/project/pull/25']) {
    assert.equal(context.prIdentity(value), '');
  }
});

test('selects the exact Chromium PR tab and raises its existing window', () => {
  const f = fixture([{id: 'chrome', windows: [['https://example.org'], [`${url}/files#diff`, `${url}0`]]}]);
  assert.deepEqual(f.navigate(url), {handled: true});
  assert.deepEqual(f.actions.map(a => a.slice(0, 4)), [
    ['activeTabIndex', 'chrome', 1, 1], ['minimized', 'chrome', 1, false], ['index', 'chrome', 1, 1], ['activate', 'chrome']
  ]);
});

test('selects an existing Safari tab using Safari properties', () => {
  const f = fixture([{id: 'safari', safari: true, windows: [[url]]}]);
  assert.deepEqual(f.navigate(url), {handled: true});
  assert.deepEqual(f.actions.map(a => a[0]), ['currentTab', 'miniaturized', 'index', 'activate']);
});

test('new-tab action prefers a PR window over the default browser and never reuses the target', () => {
  const f = fixture([{id: 'default', windows: [['https://example.org']]}, {id: 'chrome', windows: [[url]]}]);
  assert.deepEqual(f.navigate(url, true, 'default'), {handled: true});
  assert.deepEqual(f.actions[0], ['tab', 'chrome', 0, url]);
  assert.equal(f.actions.filter(a => a[0] === 'tab').length, 1);
});

test('unmatched PR opens beside other PRs, then falls back to a default-browser window', () => {
  const withPRs = fixture([{id: 'chrome', windows: [[`${url}0`]]}]);
  assert.deepEqual(withPRs.navigate(url), {handled: true});
  assert.equal(withPRs.actions[0][0], 'tab');
  const noPRs = fixture([{id: 'default', windows: [['https://example.org']]}]);
  assert.deepEqual(noPRs.navigate(url, false, 'default'), {handled: true});
  assert.equal(noPRs.actions[0][0], 'tab');
});

test('denied or absent browsers request safe default-browser fallback without navigation', () => {
  const denied = fixture([{id: 'chrome', denied: true}]);
  assert.deepEqual(denied.navigate(url), {handled: false, unavailable: true});
  assert.equal(denied.actions.length, 0);
  const none = fixture([{id: 'chrome', running: false}]);
  assert.deepEqual(none.navigate(url), {handled: false, unavailable: false});
  assert.equal(none.actions.length, 0);
});

test('uncertain navigation errors never request fallback that could duplicate a tab', () => {
  const f = fixture([{id: 'chrome', windows: [[url]], failActivate: true}]);
  assert.ok(f.navigate(url).error);
  assert.ok(f.navigate(url, true).error);
  assert.equal(f.actions.filter(a => a[0] === 'tab').length, 1);
});
