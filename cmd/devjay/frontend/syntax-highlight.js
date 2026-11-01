// Small lexical highlighting for source review, with no grammar downloads or
// HTML parsing. Unknown and unusually large inputs remain ordinary source text.
const syntaxInputLimit = 256 * 1024;
const syntaxLineLimit = 8192;
const syntaxTokenLimit = 30000;
const syntaxKinds = new Set(['plain', 'comment', 'string', 'keyword', 'number', 'type', 'function', 'property', 'tag', 'attribute', 'operator', 'constant', 'punctuation']);
const syntaxWords = words => new Set(words.split(/\s+/).filter(Boolean));
const syntaxKeywords = {
  go: 'break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var',
  javascript: 'as async await break case catch class const continue debugger default delete do else export extends finally for from function get if import in instanceof let new of return set static super switch this throw try typeof var void while with yield',
  typescript: 'abstract any as asserts async await boolean break case catch class const constructor continue declare default delete do else enum export extends finally for from function get if implements import in infer instanceof interface is keyof let module namespace never new number object of private protected public readonly require return satisfies set static string super switch symbol this throw try type typeof unique unknown var void while with yield',
  python: 'and as assert async await break case class continue def del elif else except finally for from global if import in is lambda match nonlocal not or pass raise return try while with yield',
  c: 'auto break case char const continue default do double else enum extern float for goto if inline int long register restrict return short signed sizeof static struct switch typedef union unsigned void volatile while _Bool _Complex _Atomic',
  cpp: 'alignas alignof asm auto bool break case catch char char16_t char32_t class concept const consteval constexpr constinit const_cast continue co_await co_return co_yield decltype default delete do double dynamic_cast else enum explicit export extern float for friend goto if inline int long mutable namespace new noexcept nullptr operator private protected public register reinterpret_cast requires return short signed sizeof static static_assert static_cast struct switch template this thread_local throw try typedef typeid typename union unsigned using virtual void volatile wchar_t while',
  objectivec: 'auto break case catch char class const continue default do double else enum extern float for goto id if import in inline int interface long nonatomic nullable nullable instancetype property protocol readonly readwrite register return self short signed sizeof static strong struct super switch synthesize typedef union unsigned void volatile weak while implementation end selector autoreleasepool dynamic',
  java: 'abstract assert boolean break byte case catch char class const continue default do double else enum extends final finally float for if implements import instanceof int interface long native new package private protected public record return sealed short static strictfp super switch synchronized this throw throws transient try var void volatile while yield',
  csharp: 'abstract as async await base bool break byte case catch char checked class const continue decimal default delegate do double else enum event explicit extern finally fixed float for foreach goto if implicit in int interface internal is lock long namespace new null object operator out override params private protected public readonly record ref return sbyte sealed short sizeof stackalloc static string struct switch this throw try typeof uint ulong unchecked unsafe ushort using var virtual void volatile while yield',
  swift: 'actor any as associatedtype async await borrowing break case catch class consuming continue convenience default defer deinit didSet distributed do dynamic else enum extension fallthrough fileprivate final for func get guard if import in indirect infix init inout internal is isolated lazy let mutating nonisolated nonmutating open operator optional override postfix precedencegroup prefix private protocol public repeat required rethrows return self set some static struct subscript super switch throws throw try typealias unowned var weak where while willSet',
  rust: 'as async await break const continue crate dyn else enum extern fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait type unsafe use where while',
  shell: 'case do done elif else esac export fi for function if in local readonly return select then time until while',
  sql: 'add all alter and as asc between by case cast check column constraint create cross current_date current_time database default delete desc distinct drop else end except exists false foreign from full grant group having in index inner insert intersect into is join key left like limit not null offset on or order outer primary references returning right select set table then true union unique update using values view when where with',
  css: 'important inherit initial none unset auto block inline flex grid relative absolute fixed sticky transparent',
  yaml: '', toml: '', json: '', html: '', xml: '', markdown: '',
  docker: 'add arg cmd copy entrypoint env expose from healthcheck label maintainer onbuild run shell stopsignal user volume workdir',
};
const syntaxKeywordSets = Object.fromEntries(Object.entries(syntaxKeywords).map(([name, words]) => [name, syntaxWords(words)]));
const syntaxTypes = syntaxWords('bool boolean byte char complex64 complex128 double error float float32 float64 int int8 int16 int32 int64 integer long number rune short size_t string uint uint8 uint16 uint32 uint64 uintptr u8 u16 u32 u64 usize i8 i16 i32 i64 isize f32 f64 str String Int Float Double Bool Any Object Array Promise Map Set List Dictionary Optional Result Option Vec');
const syntaxConstants = syntaxWords('true false null nil undefined None True False NULL NaN Infinity iota');
const syntaxExtensions = {
  go:'go', js:'javascript', mjs:'javascript', cjs:'javascript', jsx:'javascript', ts:'typescript', mts:'typescript', cts:'typescript', tsx:'typescript',
  py:'python', pyw:'python', pyi:'python', c:'c', h:'c', cc:'cpp', cpp:'cpp', cxx:'cpp', hpp:'cpp', hh:'cpp', hxx:'cpp', m:'objectivec', mm:'objectivec', java:'java', cs:'csharp', swift:'swift', rs:'rust',
  json:'json', jsonc:'json', css:'css', scss:'css', less:'css', html:'html', htm:'html', xml:'xml', svg:'xml', plist:'xml', xhtml:'xml',
  sh:'shell', bash:'shell', zsh:'shell', fish:'shell', yml:'yaml', yaml:'yaml', toml:'toml', sql:'sql', md:'markdown', markdown:'markdown', mdx:'markdown', dockerfile:'docker',
};

export function syntaxLanguage(path = '', text = '') {
  const filename = path.split(/[\\/]/).at(-1) || '';
  const lower = filename.toLowerCase();
  if (lower === 'dockerfile' || lower.startsWith('dockerfile.')) return 'docker';
  if (['makefile', 'gnumakefile', '.bashrc', '.bash_profile', '.zshrc', '.zprofile', '.profile'].includes(lower)) return 'shell';
  const extension = filename.includes('.') ? lower.slice(lower.lastIndexOf('.') + 1) : '';
  if (syntaxExtensions[extension]) return syntaxExtensions[extension];
  // Only executable-style names without an extension use a shebang fallback.
  // A prose file mentioning a command must never select a source language.
  if (!extension && text.startsWith('#!')) {
    const first = text.slice(0, text.indexOf('\n') < 0 ? Math.min(text.length, 512) : Math.min(text.indexOf('\n'), 512));
    if (/\bpython(?:[23](?:\.\d+)?)?\b/.test(first)) return 'python';
    if (/\b(?:node|nodejs|deno|bun)\b/.test(first)) return 'javascript';
    if (/\b(?:sh|bash|zsh|fish|ksh)\b/.test(first)) return 'shell';
  }
  return '';
}

function syntaxPlainLines(text) {
  return text.split('\n').map(line => line ? [{text:line, kind:'plain'}] : []);
}

function syntaxQuoteEnd(text, start, delimiter, multiline, escapes = true) {
  let index = start + delimiter.length;
  while (index < text.length) {
    if (!multiline && text[index] === '\n') return index;
    if (escapes && text[index] === '\\') { index = Math.min(text.length, index + 2); continue; }
    if (text.startsWith(delimiter, index)) return index + delimiter.length;
    index++;
  }
  return index;
}

// Tokens on a source line never contain a newline. Empty source lines and the
// empty final line after a terminal newline are represented by empty arrays.
export function syntaxLines(path, text = '') {
  text = String(text);
  const language = syntaxLanguage(path, text);
  if (!language || text.length > syntaxInputLimit || text.split('\n').some(line => line.length > syntaxLineLimit)) return syntaxPlainLines(text);
  const lines = [[]];
  let count = 0, overflow = false, index = 0, lineStart = 0, lineHasCode = false, inTag = false, tagName = false, markdownFence = '';
  const emit = (start, end, kind = 'plain') => {
    if (end <= start || overflow) return;
    const chunks = text.slice(start, end).split('\n');
    for (let part = 0; part < chunks.length; part++) {
      if (part) lines.push([]);
      const chunk = chunks[part];
      if (!chunk) continue;
      const line = lines.at(-1), previous = line.at(-1);
      if (previous?.kind === kind) previous.text += chunk;
      else { line.push({text:chunk, kind}); if (++count > syntaxTokenLimit) { overflow = true; return; } }
    }
  };
  const advance = (end, kind) => {
    emit(index, end, kind);
    const consumed = text.slice(index, end), newline = consumed.lastIndexOf('\n');
    if (newline >= 0) { lineStart = index + newline + 1; lineHasCode = /\S/.test(consumed.slice(newline + 1)); }
    else if (!lineHasCode) lineHasCode = /\S/.test(consumed);
    index = end;
  };
  const lineEnd = () => { const end = text.indexOf('\n', index); return end < 0 ? text.length : end; };
  const nextNonspace = end => { while (end < text.length && (text[end] === ' ' || text[end] === '\t' || text[end] === '\r')) end++; return text[end] || ''; };
  const word = /[A-Za-z_$][A-Za-z0-9_$]*/y;
  const markupWord = /[A-Za-z_:][A-Za-z0-9_:.-]*/y;
  const number = /(?:0[xX][\da-fA-F_]+|0[bB][01_]+|0[oO][0-7_]+|(?:\d[\d_]*(?:\.[\d_]*)?|\.\d[\d_]*)(?:[eE][+-]?\d[\d_]*)?)/y;
  const markup = language === 'html' || language === 'xml';
  const hashComments = ['python', 'shell', 'yaml', 'toml', 'docker'].includes(language);
  const slashComments = ['go', 'javascript', 'typescript', 'c', 'cpp', 'objectivec', 'java', 'csharp', 'swift', 'rust', 'json'].includes(language);
  const blockComments = slashComments || language === 'css' || language === 'sql';
  const keywords = syntaxKeywordSets[language];
  while (index < text.length && !overflow) {
    const char = text[index], next = text[index + 1];
    if (char === '\n') { advance(index + 1); continue; }
    if (language === 'markdown') {
      if (index === lineStart) {
        const end = lineEnd(), content = text.slice(index, end), fence = /^ {0,3}(`{3,}|~{3,})/.exec(content);
        if (fence) { markdownFence = markdownFence ? '' : fence[1][0]; advance(end, 'punctuation'); continue; }
        if (markdownFence) { advance(end, 'string'); continue; }
        if (/^ {0,3}#{1,6}(?:\s|$)/.test(content)) { advance(end, 'keyword'); continue; }
      }
      if (char === '`') { advance(syntaxQuoteEnd(text, index, '`', false, false), 'string'); continue; }
      advance(index + 1); continue;
    }
    if (markup) {
      if (text.startsWith('<!--', index)) { const end = text.indexOf('-->', index + 4); advance(end < 0 ? text.length : end + 3, 'comment'); continue; }
      if (text.startsWith('<![CDATA[', index)) { const end = text.indexOf(']]>', index + 9); advance(end < 0 ? text.length : end + 3, 'string'); continue; }
      if (char === '<' && /[A-Za-z!?/]/.test(next || '')) { inTag = true; tagName = true; advance(index + 1, 'punctuation'); continue; }
      if (inTag && char === '>') { inTag = false; advance(index + 1, 'punctuation'); continue; }
      if (inTag && (char === '"' || char === "'")) { advance(syntaxQuoteEnd(text, index, char, true, false), 'string'); continue; }
      if (inTag) {
        markupWord.lastIndex = index;
        const match = markupWord.exec(text);
        if (match) { advance(markupWord.lastIndex, tagName ? 'tag' : 'attribute'); tagName = false; continue; }
        advance(index + 1, /[=/!?]/.test(char) ? 'punctuation' : 'plain'); continue;
      }
      advance(index + 1); continue;
    }
    if ((slashComments && char === '/' && next === '/') || (language === 'sql' && char === '-' && next === '-') ||
        (hashComments && char === '#' && (language !== 'yaml' || index === lineStart || /\s/.test(text[index - 1])))) {
      advance(lineEnd(), 'comment'); continue;
    }
    if (blockComments && char === '/' && next === '*') {
      const end = text.indexOf('*/', index + 2); advance(end < 0 ? text.length : end + 2, 'comment'); continue;
    }
    if (language === 'python' && (text.startsWith('"""', index) || text.startsWith("'''", index))) {
      advance(syntaxQuoteEnd(text, index, text.slice(index, index + 3), true), 'string'); continue;
    }
    if (char === '"' || char === "'" || (char === '`' && ['go', 'javascript', 'typescript', 'shell'].includes(language))) {
      // Rust lifetimes are not character literals.
      if (language === 'rust' && char === "'" && /[A-Za-z_]/.test(next || '')) {
        word.lastIndex = index + 1; word.exec(text);
        if (text[word.lastIndex] !== "'") { advance(index + 1, 'punctuation'); continue; }
      }
      const end = syntaxQuoteEnd(text, index, char, char === '`' || language === 'shell', !(char === '`' && language === 'go') && !(language === 'shell' && char === "'"));
      advance(end, language === 'json' && char === '"' && nextNonspace(end) === ':' ? 'property' : 'string'); continue;
    }
    if (language === 'css' && char === '#') {
      let end = index + 1; while (end < text.length && /[\da-fA-F]/.test(text[end])) end++;
      if ([3, 4, 6, 8].includes(end - index - 1) && !/[\w-]/.test(text[end] || '')) { advance(end, 'number'); continue; }
    }
    if ((language === 'c' || language === 'cpp' || language === 'objectivec') && char === '#' && !lineHasCode) {
      let end = index + 1; while (end < text.length && /[a-zA-Z]/.test(text[end])) end++;
      advance(end, 'keyword'); continue;
    }
    number.lastIndex = index;
    const numeric = number.exec(text);
    if (numeric) { advance(number.lastIndex, 'number'); continue; }
    word.lastIndex = index;
    const match = word.exec(text);
    if (match) {
      let end = word.lastIndex;
      // CSS declarations and YAML/TOML keys commonly contain dashes or dots.
      if (['css', 'yaml', 'toml'].includes(language)) while (end < text.length && /[A-Za-z0-9_.-]/.test(text[end])) end++;
      const value = text.slice(index, end), folded = ['sql', 'css', 'docker'].includes(language) ? value.toLowerCase() : value;
      let kind = 'plain';
      if (syntaxConstants.has(value) || (language === 'sql' && ['true', 'false', 'null'].includes(folded))) kind = 'constant';
      else if (keywords.has(folded)) kind = 'keyword';
      else if (syntaxTypes.has(value)) kind = 'type';
      else if ((['json', 'yaml', 'css', 'javascript', 'typescript'].includes(language) && nextNonspace(end) === ':') || (language === 'toml' && nextNonspace(end) === '=')) kind = 'property';
      else if (nextNonspace(end) === '(') kind = 'function';
      else if (/^[A-Z][A-Z0-9_]+$/.test(value)) kind = 'constant';
      else if (/^[A-Z][A-Za-z0-9_]*$/.test(value) && !['json', 'yaml', 'toml', 'sql', 'shell', 'docker', 'css'].includes(language)) kind = 'type';
      advance(end, kind); continue;
    }
    advance(index + 1, /[+*=!<>|&%~?:^-]/.test(char) ? 'operator' : /[{}[\]();,.]/.test(char) ? 'punctuation' : 'plain');
  }
  return overflow ? syntaxPlainLines(text) : lines;
}

// Render tokens through textContent only. Range slicing lets a diff renderer
// keep its independent inline-change marks around the same source characters.
export function appendSyntax(parent, text, tokens, from = 0, to = text.length) {
  const document = parent.ownerDocument || globalThis.document;
  from = Math.max(0, Math.min(text.length, from));
  to = Math.max(from, Math.min(text.length, to));
  const append = (value, kind = 'plain') => {
    if (!value) return;
    const span = document.createElement('span');
    if (kind !== 'plain' && syntaxKinds.has(kind)) span.className = `syntax-${kind}`;
    span.textContent = value; parent.append(span);
  };
  if (!Array.isArray(tokens) || tokens.some(token => typeof token?.text !== 'string') || tokens.map(token => token.text).join('') !== text) {
    append(text.slice(from, to)); return parent;
  }
  let offset = 0;
  for (const token of tokens) {
    const end = offset + token.text.length;
    if (end > from && offset < to) append(token.text.slice(Math.max(0, from - offset), Math.min(token.text.length, to - offset)), token.kind);
    offset = end;
    if (offset >= to) break;
  }
  return parent;
}
