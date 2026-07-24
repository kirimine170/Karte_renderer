import assert from "node:assert/strict";
import { readFile, stat } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import katex from "katex";
import { parse as parseYAML } from "yaml";

const root = path.resolve("testdata/karte-format/complex");

test("complex karte-format fixture has valid linked resources", async () => {
  const manifest = parseYAML(await readFile(path.join(root, "manifest.yaml"), "utf8"));

  assert.equal(manifest.karte, 0.1);
  assert.equal(manifest.kind, "document");
  assert.equal(manifest.entry, "content/document.md");
  assert.equal(Object.keys(manifest.resources).length, 5);

  const referenced = new Set();
  await walkMarkdown(resolveInside(root, manifest.entry), referenced, new Set());

  for (const [id, resource] of Object.entries(manifest.resources)) {
    const resourcePath = resolveInside(root, resource.path);
    assert.equal((await stat(resourcePath)).isFile(), true, `${id} must be a file`);
    assert.equal(referenced.has(normalizeRelative(resourcePath)), true, `${id} must be referenced`);

    const content = await readFile(resourcePath);
    if (resource.type === "csv") {
      const records = parseCSV(content.toString("utf8"));
      assert.ok(records.length >= 2, `${id} must contain a header and data`);
      assert.ok(records.every((row) => row.length === records[0].length), `${id} has uneven rows`);
    } else if (resource.type === "tex") {
      assert.notEqual(content.toString("utf8").trim(), "", `${id} must not be empty`);
    } else if (resource.type === "webp") {
      assert.deepEqual(content.subarray(0, 4), Buffer.from("RIFF"));
      assert.deepEqual(content.subarray(8, 12), Buffer.from("WEBP"));
      assert.deepEqual(webpSize(content), { width: 640, height: 360 });
    } else {
      assert.fail(`unsupported resource type: ${resource.type}`);
    }
  }
});

test("complex TeX resources render with KaTeX", async () => {
  const formulas = [
    ["math/energy.tex", false],
    ["math/weighted-sum.tex", true],
  ];

  for (const [relative, displayMode] of formulas) {
    const expression = (await readFile(path.join(root, relative), "utf8")).trim();
    const rendered = katex.renderToString(expression, {
      displayMode,
      output: "htmlAndMathml",
      throwOnError: true,
    });
    assert.match(rendered, /class="katex-mathml"/);
    assert.match(rendered, /class="katex-html"/);
    assert.doesNotMatch(rendered, /class="katex-error"/);
  }
});

async function walkMarkdown(file, referenced, active) {
  const relative = normalizeRelative(file);
  assert.equal(active.has(relative), false, `cyclic Markdown import at ${relative}`);
  active.add(relative);

  const markdown = await readFile(file, "utf8");
  const importPattern = /^@import\(([^)]*)\)\s*$/gm;
  for (const match of markdown.matchAll(importPattern)) {
    const attrs = parseAttrs(match[1]);
    assert.ok(attrs.type, `import in ${relative} needs a type`);
    assert.ok(attrs.path, `import in ${relative} needs a path`);
    const imported = resolveInside(path.dirname(file), attrs.path);
    await stat(imported);

    if (attrs.type === "md" || attrs.type === "markdown") {
      await walkMarkdown(imported, referenced, active);
    } else {
      referenced.add(normalizeRelative(imported));
    }
  }

  for (const match of markdown.matchAll(/!\[[^\]]*]\(([^)\s]+\.webp)\)/g)) {
    const image = resolveInside(path.dirname(file), match[1]);
    await stat(image);
    referenced.add(normalizeRelative(image));
  }
  active.delete(relative);
}

function parseAttrs(source) {
  const attrs = {};
  const pattern = /(\w+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s]+))/g;
  for (const match of source.matchAll(pattern)) {
    attrs[match[1]] = match[2] ?? match[3] ?? match[4];
  }
  return attrs;
}

function resolveInside(base, relative) {
  const resolved = path.resolve(base, relative);
  assert.ok(resolved === root || resolved.startsWith(`${root}${path.sep}`), `path escapes fixture: ${relative}`);
  return resolved;
}

function normalizeRelative(file) {
  return path.relative(root, file).split(path.sep).join("/");
}

function parseCSV(source) {
  const rows = [];
  let row = [];
  let field = "";
  let quoted = false;

  for (let index = 0; index < source.length; index += 1) {
    const char = source[index];
    if (quoted && char === '"' && source[index + 1] === '"') {
      field += '"';
      index += 1;
    } else if (char === '"') {
      quoted = !quoted;
    } else if (char === "," && !quoted) {
      row.push(field);
      field = "";
    } else if ((char === "\n" || char === "\r") && !quoted) {
      if (char === "\r" && source[index + 1] === "\n") index += 1;
      row.push(field);
      if (row.some((value) => value !== "")) rows.push(row);
      row = [];
      field = "";
    } else {
      field += char;
    }
  }
  assert.equal(quoted, false, "CSV has an unclosed quoted field");
  if (field !== "" || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return rows;
}

function webpSize(buffer) {
  const chunk = buffer.toString("ascii", 12, 16);
  if (chunk === "VP8 ") {
    assert.deepEqual(buffer.subarray(23, 26), Buffer.from([0x9d, 0x01, 0x2a]));
    return {
      width: buffer.readUInt16LE(26) & 0x3fff,
      height: buffer.readUInt16LE(28) & 0x3fff,
    };
  }
  if (chunk === "VP8X") {
    return {
      width: buffer.readUIntLE(24, 3) + 1,
      height: buffer.readUIntLE(27, 3) + 1,
    };
  }
  assert.fail(`unsupported WebP chunk: ${chunk}`);
}
