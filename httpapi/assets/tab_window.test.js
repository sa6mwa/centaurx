const test = require('node:test');
const assert = require('node:assert/strict');

const { resolveWindow } = require('./tab_window.js');

test('resolveWindow keeps window until active leaves', () => {
  const widths = [6, 6, 6, 6, 6];
  const barWidth = 22;
  const indicator = 2;
  const gap = 0;
  let start = 0;

  let window = resolveWindow(widths, 0, start, barWidth, indicator, indicator, gap);
  assert.equal(window.start, 0);
  assert.equal(window.end, 3);

  window = resolveWindow(widths, 1, window.start, barWidth, indicator, indicator, gap);
  assert.equal(window.start, 0);

  window = resolveWindow(widths, 2, window.start, barWidth, indicator, indicator, gap);
  assert.equal(window.start, 0);

  window = resolveWindow(widths, 3, window.start, barWidth, indicator, indicator, gap);
  assert.equal(window.start, 1);

  window = resolveWindow(widths, 4, window.start, barWidth, indicator, indicator, gap);
  assert.equal(window.start, 2);

  window = resolveWindow(widths, 1, window.start, barWidth, indicator, indicator, gap);
  assert.equal(window.start, 1);
});

test('resolveWindow shows all tabs when they fit', () => {
  const widths = [5, 5, 5];
  const barWidth = 20;
  const indicator = 2;
  const window = resolveWindow(widths, 1, 0, barWidth, indicator, indicator, 0);
  assert.equal(window.start, 0);
  assert.equal(window.end, 3);
  assert.equal(window.leftHidden, false);
  assert.equal(window.rightHidden, false);
});
