// Ignored-file patterns are shared by Review and Changes/History windows.
// They only hide rows in DevJay; Git ignore rules and files are unchanged.
export const defaultReviewIgnorePatterns = '*_test.go\n*_gen.go';

// Filename patterns apply in every folder; path patterns are repository-relative.
// ** crosses folders, while * and ? match within one path component.
export function reviewIgnoreMatchers(value) {
  return value.split(/[,\n]/).map(pattern => pattern.trim()).filter(Boolean).map(pattern => {
    const rooted = pattern.startsWith('/');
    pattern = pattern.replace(/^\//, '');
    const directory = pattern.endsWith('/');
    if (directory) pattern = pattern.slice(0, -1);
    const anywhere = !rooted && !pattern.includes('/');
    let source = '';
    for (let i = 0; i < pattern.length; i++) {
      const char = pattern[i];
      if (char === '*') {
        if (pattern[i + 1] === '*') {
          while (pattern[i + 1] === '*') i++;
          if (pattern[i + 1] === '/') { source += '(?:.*/)?'; i++; }
          else source += '.*';
        } else source += '[^/]*';
      } else if (char === '?') source += '[^/]';
      else source += char.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    }
    return new RegExp((anywhere ? '(?:^|/)' : '^') + source + (directory ? '/.*$' : '$'));
  });
}

export function reviewFileIgnored(file, matchers) {
  // A rename out of an ignored name must remain reviewable.
  return matchers.some(pattern => pattern.test(file.path));
}

// Patterns are remembered per repository folder. Storage can be unavailable;
// the defaults and the current window's edits still apply.
export function ignorePatternsStorageKey(path) {
  return 'devjay-branch-review-ignore:' + (path || '');
}
// Null means nothing is stored or storage is unavailable.
export function storedIgnorePatterns(key) {
  try { return localStorage.getItem(key); } catch { return null; }
}
export function saveIgnorePatterns(key, value) {
  try { localStorage.setItem(key, value); } catch { /* The live patterns still apply to this window. */ }
}
