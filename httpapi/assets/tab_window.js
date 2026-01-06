/* eslint-disable no-var */
(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.CentaurxTabWindow = factory();
  }
})(typeof globalThis !== 'undefined' ? globalThis : this, function () {
  function clampStart(start, n) {
    if (start < 0) return 0;
    if (start >= n) return n - 1;
    return start;
  }

  function normalizeActive(activeIndex, n) {
    if (activeIndex < 0 || activeIndex >= n) return 0;
    return activeIndex;
  }

  function fitForward(widths, start, avail, gap) {
    var n = widths.length;
    if (n === 0) return 0;
    if (start < 0) start = 0;
    if (start >= n) return n;
    if (avail < 1) avail = 1;
    var sum = 0;
    var end = start;
    for (var i = start; i < n; i++) {
      var add = widths[i];
      if (i > start) add += gap;
      if (sum + add > avail) {
        if (i === start) return start + 1;
        break;
      }
      sum += add;
      end = i + 1;
    }
    if (end === start) end = start + 1;
    return end;
  }

  function fitBackward(widths, end, avail, gap) {
    var n = widths.length;
    if (n === 0) return 0;
    if (end < 1) return 0;
    if (end > n) end = n;
    if (avail < 1) avail = 1;
    var sum = 0;
    var start = end;
    for (var i = end - 1; i >= 0; i--) {
      var add = widths[i];
      if (i < end - 1) add += gap;
      if (sum + add > avail) {
        if (i === end - 1) return end - 1;
        break;
      }
      sum += add;
      start = i;
    }
    if (start === end) start = end - 1;
    return start;
  }

  function windowFromStart(widths, start, barWidth, leftWidth, rightWidth, gap) {
    var n = widths.length;
    if (n === 0) {
      return { start: 0, end: 0, leftHidden: false, rightHidden: false };
    }
    start = clampStart(start, n);
    var leftHidden = start > 0;
    var rightHidden = false;
    var end = start + 1;
    for (var i = 0; i < 3; i++) {
      var avail = barWidth;
      if (leftHidden) avail -= leftWidth;
      if (rightHidden) avail -= rightWidth;
      if (avail < 1) avail = 1;
      end = fitForward(widths, start, avail, gap);
      rightHidden = end < n;
      leftHidden = start > 0;
    }
    return { start: start, end: end, leftHidden: leftHidden, rightHidden: rightHidden };
  }

  function windowActiveLeft(widths, activeIndex, barWidth, leftWidth, rightWidth, gap) {
    var n = widths.length;
    if (n === 0) {
      return { start: 0, end: 0, leftHidden: false, rightHidden: false };
    }
    activeIndex = normalizeActive(activeIndex, n);
    var start = activeIndex;
    var leftHidden = start > 0;
    var rightHidden = false;
    var end = start + 1;
    for (var i = 0; i < 3; i++) {
      var avail = barWidth;
      if (leftHidden) avail -= leftWidth;
      if (rightHidden) avail -= rightWidth;
      if (avail < 1) avail = 1;
      end = fitForward(widths, start, avail, gap);
      rightHidden = end < n;
      leftHidden = start > 0;
    }
    return { start: start, end: end, leftHidden: leftHidden, rightHidden: rightHidden };
  }

  function windowActiveRight(widths, activeIndex, barWidth, leftWidth, rightWidth, gap) {
    var n = widths.length;
    if (n === 0) {
      return { start: 0, end: 0, leftHidden: false, rightHidden: false };
    }
    activeIndex = normalizeActive(activeIndex, n);
    var end = Math.min(activeIndex + 1, n);
    var rightHidden = end < n;
    var leftHidden = false;
    var start = end - 1;
    for (var i = 0; i < 3; i++) {
      var avail = barWidth;
      if (leftHidden) avail -= leftWidth;
      if (rightHidden) avail -= rightWidth;
      if (avail < 1) avail = 1;
      start = fitBackward(widths, end, avail, gap);
      leftHidden = start > 0;
      rightHidden = end < n;
    }
    return { start: start, end: end, leftHidden: leftHidden, rightHidden: rightHidden };
  }

  function resolveWindow(widths, activeIndex, start, barWidth, leftWidth, rightWidth, gap) {
    var n = widths.length;
    if (n === 0) {
      return { start: 0, end: 0, leftHidden: false, rightHidden: false };
    }
    if (barWidth < 1) barWidth = 1;
    if (!Number.isFinite(gap) || gap < 0) gap = 0;
    activeIndex = normalizeActive(activeIndex, n);
    start = clampStart(start, n);
    var total = 0;
    for (var i = 0; i < n; i++) total += widths[i];
    if (n > 1) total += gap * (n - 1);
    if (total <= barWidth) {
      return { start: 0, end: n, leftHidden: false, rightHidden: false };
    }
    var window = windowFromStart(widths, start, barWidth, leftWidth, rightWidth, gap);
    if (activeIndex < window.start) {
      return windowActiveLeft(widths, activeIndex, barWidth, leftWidth, rightWidth, gap);
    }
    if (activeIndex >= window.end) {
      return windowActiveRight(widths, activeIndex, barWidth, leftWidth, rightWidth, gap);
    }
    return window;
  }

  return {
    resolveWindow: resolveWindow,
  };
});
