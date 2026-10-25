import test from 'node:test';
import assert from 'node:assert/strict';
import { cropRect, textStats, postRatio, validatePost, createManifest, parseManifest } from '../instagram/web/renderer.mjs';

const image = (overrides = {}) => ({ name: 'example.jpg', width: 1080, height: 1350, type: 'image/jpeg', bytes: 500000, crop: { x: 0.5, y: 0.5, zoom: 1 }, ...overrides });
const state = (overrides = {}) => ({ mode: 'app', caption: '', images: [image()], ratio: 0.8, ...overrides });
test('a square image loses equal margins in a centered portrait crop', () => {
  const rect = cropRect(1080, 1080, 0.75);
  assert.equal(rect.sx, 135); assert.equal(rect.sy, 0);
  assert.equal(rect.sw, 810); assert.equal(rect.sh, 1080); assert.equal(rect.retained, 0.75);
});
test('zoom and position stay within the source image', () => {
  for (const ratio of [0.75, 0.8, 1, 1.91]) for (const zoom of [1, 1.5, 3]) for (const x of [0, 0.5, 1]) for (const y of [0, 0.5, 1]) {
    const c = cropRect(1600, 900, ratio, { zoom, x, y });
    assert.ok(c.sx >= 0 && c.sy >= 0 && c.sx + c.sw <= 1600 + 1e-8 && c.sy + c.sh <= 900 + 1e-8);
    assert.ok(Math.abs(c.sw / c.sh - ratio) < 1e-8);
  }
});
test('invalid crop dimensions are rejected', () => {
  for (const dimensions of [[0, 10, 1], [100, NaN, 1], [10, 10, -1], [10, 10, Infinity]]) assert.throws(() => cropRect(...dimensions), RangeError);
});
test('automatic aspect respects different app and API lower bounds', () => {
  assert.equal(postRatio('auto', 'app', image({ width: 1080, height: 1920 })), 0.75);
  assert.equal(postRatio('auto', 'api', image({ width: 1080, height: 1920 })), 0.8);
  assert.equal(postRatio('auto', 'app', image({ width: 3000, height: 1000 })), 1.91);
  assert.equal(postRatio('1', 'app', image()), 1);
});
test('code points and visible graphemes are counted separately with normalized newlines', () => {
  const stats = textStats('あ👩‍💻\r\nX');
  assert.equal(stats.characters, 6); assert.equal(stats.graphemes, 4); assert.equal(stats.lines, 2);
});
test('Unicode hashtags and usernames are distinguished from email addresses', () => {
  const stats = textStats('#暮らし #Karte_Studio\n@first.name @second test@example.com');
  assert.equal(stats.hashtags, 2); assert.equal(stats.mentions, 2);
});
test('caption boundary accepts 2200 code points and flags the next character', () => {
  assert.ok(!validatePost(state({ caption: 'あ'.repeat(2200) })).issues.some(i => i.code === 'caption'));
  assert.ok(validatePost(state({ caption: 'あ'.repeat(2201) })).issues.some(i => i.code === 'caption'));
});
test('API and app image count assumptions differ', () => {
  const images = Array.from({ length: 11 }, () => image());
  assert.ok(!validatePost(state({ images })).issues.some(i => i.code === 'count'));
  assert.ok(validatePost(state({ images, mode: 'api' })).issues.some(i => i.code === 'count'));
});
test('API source format and file size require attention，without conflating app restrictions', () => {
  const images = [image({ type: 'image/png', bytes: 9 * 1024 * 1024 })];
  assert.ok(validatePost(state({ mode: 'api', images })).issues.some(i => i.code === 'format'));
  assert.ok(validatePost(state({ mode: 'api', images })).issues.some(i => i.code === 'size'));
  assert.ok(!validatePost(state({ images })).issues.some(i => ['format', 'size'].includes(i.code)));
});
test('exported manifest round trips coordinates，without image contents', () => {
  const input = { mode: 'api', username: 'karte', location: 'Kyoto', caption: 'test 🌿', aspect: '0.8', devices: ['compact'], textScale: 125, collapsedLines: 3, gridRatio: 1, customWidth: 340, customHeight: 700 };
  const manifest = createManifest(input, [image({ crop: { x: 0, y: 1, zoom: 2 } })], 0.8);
  const parsed = parseManifest(JSON.parse(JSON.stringify(manifest)));
  assert.equal(parsed.caption, input.caption); assert.deepEqual(parsed.pendingImages[0].crop, { x: 0, y: 1, zoom: 2 });
  assert.equal(parsed.textScale, 125); assert.equal(parsed.gridRatio, 1); assert.equal(manifest.images[0].sourceRect.sx, 0);
  assert.ok(!JSON.stringify(manifest).includes('data:image'));
});
test('draft import rejects unsupported schemas and excessive media，and clamps settings', () => {
  assert.throws(() => parseManifest({ schema: 'other' }));
  assert.throws(() => parseManifest({ schema: 'karte.instagram-draft', version: 1, caption: '', images: Array(21).fill({}) }));
  const parsed = parseManifest({ schema: 'karte.instagram-draft', version: 1, caption: '', images: [], simulation: { textScale: 999, devices: ['unknown'], customWidth: 5 } });
  assert.equal(parsed.textScale, 150); assert.equal(parsed.customWidth, 280); assert.ok(parsed.devices.length > 0);
  assert.throws(() => parseManifest({ schema: 'karte.instagram-draft', version: 1, caption: '', images: [null] }));
  const centered = parseManifest({ schema: 'karte.instagram-draft', version: 1, caption: '', images: [{ filename: 'a.jpg' }] });
  assert.deepEqual(centered.pendingImages[0].crop, { x: 0.5, y: 0.5, zoom: 1 });
});
test('profile crop is applied to the exported post rather than the raw source', () => {
  const first = cropRect(1600, 900, 0.8);
  const grid = cropRect(1080, 1350, 0.75);
  assert.equal(first.sw, 720); assert.equal(first.sh, 900);
  assert.equal(grid.sw, 1012.5); assert.ok(Math.abs(grid.retained - 0.9375) < 1e-8);
});
