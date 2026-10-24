import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';

const installer = resolve('scripts/install.sh');
function fixture(t, mode = '', architecture = 'arm64') {
  const root = mkdtempSync(join(tmpdir(), 'devjay-release-test-'));
  t.after(() => rmSync(root, {recursive:true, force:true}));
  const bin = join(root, 'bin'), dest = join(root, 'Applications');
  mkdirSync(bin); mkdirSync(dest);
  const old = join(dest, 'DevJay.app'); mkdirSync(old);
  writeFileSync(join(old, 'original'), 'keep me');
  const digest = createHash('sha256').update('archive').digest('hex');
  const mock = (name, script) => writeFileSync(join(bin, name), '#!/bin/bash\nset -eu\n' + script, {mode:0o755});
  mock('uname', `if [[ "$1" == -s ]]; then echo Darwin; else echo ${architecture === 'arm64' ? 'arm64' : 'x86_64'}; fi`);
  mock('sysctl', `echo ${architecture === 'rosetta' ? '1' : '0'}`);
  const downloadArchitecture = architecture === 'amd64' ? 'amd64' : 'arm64';
  mock('curl', `url=''; out=''; while (($#)); do case "$1" in -o) out="$2"; shift;; https:*) url="$1";; esac; shift; done
case "$url" in
*/version.txt) echo 0.8.0;;
*.sha256) echo '${mode === 'corrupt' ? '0'.repeat(64) : digest}  DevJay-0.8.0-macos-${downloadArchitecture}.zip' > "$out";;
*) printf '%s' "$url" > "$DEVJAY_INSTALL_DIR/download-url"; printf archive > "$out";; esac`);
  // PlistBuddy and shasum remain real: validate the downloaded bundle metadata.
  mock('ditto', `mkdir -p "$4/DevJay.app"
cat > "$4/DevJay.app/Info.plist" <<'PLIST'
<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>io.teamconclude.devjay</string><key>CFBundleShortVersionString</key><string>0.8.0</string></dict></plist>
PLIST
mkdir "$4/DevJay.app/Contents"
mv "$4/DevJay.app/Info.plist" "$4/DevJay.app/Contents/Info.plist"`);
  mock('codesign', mode === 'signature' ? 'exit 1' : 'exit 0');
  mock('osascript', mode === 'automation' ? 'exit 1' : `echo ${mode === 'running' ? 'true' : 'false'}`);
  mock('sleep', 'exit 0');
  mock('open', 'printf opened > "$DEVJAY_INSTALL_DIR/opened"');
  if (mode === 'move') mock('mv', 'if [[ "$1" == */unpacked/DevJay.app ]]; then exit 1; fi\nexec /bin/mv "$@"');
  return {dest, run:() => spawnSync('/bin/bash', [installer], {encoding:'utf8', env:{...process.env, PATH:bin+':'+process.env.PATH, DEVJAY_INSTALL_DIR:dest}})};
}
for (const architecture of ['arm64', 'amd64', 'rosetta']) {
  test(`terminal installer selects native architecture on ${architecture} and opens a verified bundle`, t => {
    const f = fixture(t, '', architecture), r = f.run(); assert.equal(r.status, 0, r.stderr);
    const expected = architecture === 'amd64' ? 'amd64' : 'arm64';
    assert.equal(readFileSync(join(f.dest,'download-url'),'utf8'), `https://github.com/teamconclude/DevJay/releases/download/v0.8.0/DevJay-0.8.0-macos-${expected}.zip`);
    assert.ok(existsSync(join(f.dest,'DevJay.app/Contents/Info.plist')));
    assert.ok(existsSync(join(f.dest,'opened')));
  });
}
for (const mode of ['corrupt','signature','running','automation','move']) {
  test(`terminal installer preserves previous app on ${mode} failure`, t => {
    const f = fixture(t, mode), r = f.run(); assert.notEqual(r.status, 0);
    assert.equal(readFileSync(join(f.dest,'DevJay.app/original'),'utf8'),'keep me');
    assert.ok(!existsSync(join(f.dest,'opened')));
  });
}
test('release builder rejects malformed tags before doing work', () => {
  for (const tag of ['', '0.8.0', 'v01.8.0', 'v0.8.0-beta', 'v0.8.0;exit']) {
    const r = spawnSync('/bin/bash', ['scripts/build-release.sh',tag], {encoding:'utf8',env:{...process.env,GITHUB_REF_NAME:''}});
    assert.notEqual(r.status,0); assert.match(r.stderr,/Expected a release tag/);
  }
});
