// Fixed JavaScript for Automation, executed only after a user asks to open a PR.
// It reads browser tab URLs, never page contents or JavaScript in a web page.
function prIdentity(value) {
  const match = /^https:\/\/github\.com\/([a-z0-9][a-z0-9_-]*)\/([a-z0-9_.-]+)\/pull\/([1-9][0-9]*)(?:\/[^?#]*)?(?:\?[^#]*)?(?:#.*)?$/i.exec(String(value || ''));
  if (!match || match[2] === '.' || match[2] === '..') return '';
  return (match[1] + '/' + match[2] + '/pull/' + match[3]).toLowerCase();
}

function browserWindows(browser) {
  const app = Application(browser.id);
  if (!app.running()) return [];
  // Resolve windows by stable IDs before changing front-to-back ordering.
  return app.windows().map(function (window) {
    const id = window.id();
    const stable = app.windows.byId(id);
    return {app: app, browser: browser, window: stable, tabs: stable.tabs()};
  });
}

function activatePRWindow(entry, tabIndex) {
  if (entry.browser.safari) {
    entry.window.currentTab = entry.tabs[tabIndex];
    entry.window.miniaturized = false;
  } else {
    entry.window.activeTabIndex = tabIndex + 1;
    entry.window.minimized = false;
  }
  entry.window.index = 1;
  entry.app.activate();
}

function createPRTab(entry, url) {
  const tab = entry.app.Tab({url: url});
  entry.window.tabs.push(tab);
  // Re-enumerate after insertion rather than assuming the browser's tab count.
  entry.tabs = entry.window.tabs();
  activatePRWindow(entry, entry.tabs.length - 1);
}

function navigatePR(url, newTab, browsers, defaultID) {
  const target = prIdentity(url);
  if (!target) return {error: 'This pull request URL is invalid.'};
  let unavailable = false;
  let preferred = null;
  let defaultWindow = null;
  for (let b = 0; b < browsers.length; b++) {
    let windows;
    try { windows = browserWindows(browsers[b]); }
    catch (_) { unavailable = true; continue; }
    for (let w = 0; w < windows.length; w++) {
      const entry = windows[w];
      if (!defaultWindow && entry.browser.id === defaultID) defaultWindow = entry;
      for (let t = 0; t < entry.tabs.length; t++) {
        let identity;
        try { identity = prIdentity(entry.tabs[t].url()); }
        catch (_) { unavailable = true; continue; }
        if (!identity) continue;
        if (!preferred) preferred = entry;
        if (identity === target && !newTab) {
          try { activatePRWindow(entry, t); return {handled: true}; }
          catch (_) { return {error: 'The PR tab was found, but could not be activated. Check the browser before trying again.'}; }
        }
      }
    }
  }
  const destination = preferred || defaultWindow;
  if (destination) {
    try { createPRTab(destination, url); return {handled: true}; }
    catch (_) { return {error: 'The browser did not confirm opening the new PR tab. Check it before trying again.'}; }
  }
  return {handled: false, unavailable: unavailable};
}

function run(argv) {
  ObjC.import('AppKit');
  const supported = [
    {id: 'com.apple.Safari', safari: true},
    {id: 'com.google.Chrome'},
    {id: 'com.microsoft.edgemac'},
    {id: 'com.brave.Browser'}
  ];
  const running = $.NSWorkspace.sharedWorkspace.runningApplications;
  const browsers = [];
  for (let i = 0; i < running.count; i++) {
    const native = running.objectAtIndex(i);
    const id = ObjC.unwrap(native.bundleIdentifier);
    const browser = supported.filter(function (item) { return item.id === id; })[0];
    if (browser) {
      if (native.active) browsers.unshift(browser);
      else browsers.push(browser);
    }
  }
  let defaultID = '';
  try {
    const location = $.NSWorkspace.sharedWorkspace.URLForApplicationToOpenURL($.NSURL.URLWithString(argv[0]));
    defaultID = ObjC.unwrap($.NSBundle.bundleWithURL(location).bundleIdentifier) || '';
  } catch (_) {}
  return JSON.stringify(navigatePR(argv[0], argv[1] === 'true', browsers, defaultID));
}
