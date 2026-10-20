import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
const source = await readFile(new URL('../cmd/devjay/frontend/drag-origin.js', import.meta.url), 'utf8');
const { installDragOriginGuard } = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
function windowFixture() {
  const handlers = new Map(), attributes = new Set();
  installDragOriginGuard({ addEventListener(type, fn) { handlers.set(type, fn); } }, {
    setAttribute(name) { attributes.add(name); }, removeAttribute(name) { attributes.delete(name); }
  });
  return { event(type, values = {}) { handlers.get(type)({isTrusted: true, isPrimary: true, button: 0, ...values}); }, armed() { return attributes.has('data-local-drag-press'); } };
}
test('resizing another window cannot arm the strip or Sites', () => {
  const settings = windowFixture(), strip = windowFixture(), site = windowFixture();
  settings.event('pointerdown');
  for (const destination of [strip, site]) {
    destination.event('pointermove', {buttons: 1});
    destination.event('pointermove', {buttons: 1});
    assert.equal(destination.armed(), false);
  }
});
test('a local press permits dragging and release disarms it', () => {
  const strip = windowFixture();
  strip.event('pointerdown'); strip.event('pointermove', {buttons: 1}); assert.equal(strip.armed(), true);
  strip.event('pointerup'); assert.equal(strip.armed(), false);
  strip.event('pointermove', {buttons: 1}); assert.equal(strip.armed(), false);
});
test('cancel, blur, and a released-button move clear stale gestures', () => {
  for (const type of ['pointercancel', 'blur', 'pointermove']) {
    const strip = windowFixture(); strip.event('pointerdown'); strip.event(type, {buttons: 0});
    strip.event('pointermove', {buttons: 1}); assert.equal(strip.armed(), false);
  }
});
test('right clicks and synthetic presses cannot enable window dragging', () => {
  for (const values of [{button: 2}, {isTrusted: false}, {isPrimary: false}]) {
    const strip = windowFixture(); strip.event('pointerdown', values); assert.equal(strip.armed(), false);
  }
});
