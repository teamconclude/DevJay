// Display choices remembered across windows and launches. They only change
// how DevJay presents diffs. Storage can be unavailable; callers then keep
// their defaults and the current window's choice still applies.
const viewPreferencePrefix = 'devjay-view:';

// Null means nothing usable is stored or storage is unavailable.
export function storedViewPreference(name, allowed) {
  try {
    const value = localStorage.getItem(viewPreferencePrefix + name);
    return allowed.includes(value) ? value : null;
  } catch { return null; }
}
export function saveViewPreference(name, value) {
  try { localStorage.setItem(viewPreferencePrefix + name, value); } catch { /* This window keeps the choice. */ }
}
// Unified or side-by-side diffs, shared by Review and Changes/History windows.
export const diffLayoutPreference = 'diff-layout';
export const diffLayouts = ['unified', 'split'];
