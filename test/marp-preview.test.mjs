import assert from "node:assert/strict";
import { mkdtemp, readFile, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { renderMarpPreview } from "../scripts/marp-preview.mjs";

const root = () => mkdtemp(path.join(tmpdir(), "karte-marp-preview-"));
const request = (project, source, options = {}) => ({ root: project, source, ...options });

test("renders official Marp semantics without browser JavaScript", async () => {
  const project = await root();
  const result = await renderMarpPreview(
    request(
      project,
      `---\nmarp: true\npaginate: true\nheader: Header\nfooter: Footer\n---\n<!-- _class: lead -->\n<!-- _backgroundColor: #123456 -->\n# 日本語 😀\n\n\`\`\`js\nconst separator = "---";\n\`\`\`\n\n$E=mc^2$\n\n<!-- speaker note -->\n\n---\n\n# Two\n\n---\n`.replaceAll("\n", "\r\n"),
    ),
  );
  assert.equal((result.html.match(/<section\b/g) ?? []).length, 3);
  assert.match(result.html, /data-marpit-svg/);
  assert.match(result.html, /class="lead"/);
  assert.match(result.html, /data-paginate="true"/);
  assert.match(result.html, /data-background-color="#123456"/);
  assert.match(result.html, /<header>Header<\/header>/);
  assert.match(result.html, /hljs/);
  assert.match(result.html, /MathJax/);
  assert.equal(result.html.includes("<script"), false);
  assert.equal(result.html.includes("twemoji"), false);
  assert.equal(result.comments.flat().includes("speaker note"), true);
});

test("raw HTML is disabled by default and enabled only when trusted", async () => {
  const project = await root();
  const source = '# Slide\n\n<div data-preview="yes">trusted</div>';
  const safe = await renderMarpPreview(request(project, source));
  assert.equal(safe.html.includes('<div data-preview="yes">'), false);
  assert.match(safe.html, /&lt;div/);
  const trusted = await renderMarpPreview(request(project, source, { trustedHTML: true }));
  assert.match(trusted.html, /<div data-preview="yes">trusted<\/div>/);
  await assert.rejects(
    renderMarpPreview(request(project, '# X\n<script src="https://example.test/x.js"></script>', { trustedHTML: true })),
    /script elements are not allowed/,
  );
});

test("loads custom theme sets and overrides source theme", async () => {
  const project = await root();
  await writeFile(
    path.join(project, "custom.css"),
    "/* @theme karte-custom */\nsection { background: rgb(1, 2, 3); }",
  );
  const result = await renderMarpPreview(
    request(project, "---\ntheme: default\n---\n# Slide", {
      theme: "karte-custom",
      themeSet: ["custom.css"],
    }),
  );
  assert.match(result.html, /data-theme="karte-custom"/);
  assert.match(result.css, /rgb\(1, 2, 3\)/);
});

test("enforces the project-root and no-network asset boundary", async () => {
  const project = await root();
  const outside = await root();
  await writeFile(path.join(outside, "outside.css"), "/* @theme outside */");
  await assert.rejects(
    renderMarpPreview(request(project, "# X", { themeSet: [path.join(outside, "outside.css")] })),
    /outside the Marp preview root/,
  );
  await assert.rejects(
    renderMarpPreview(request(project, "# X\n\n![image](../outside.png)")),
    /outside the Marp preview root/,
  );
  await assert.rejects(
    renderMarpPreview(request(project, "# X\n\n![image](https://example.test/x.png)")),
    /cannot use a network resource/,
  );
  await writeFile(
    path.join(project, "remote.css"),
    '/* @theme remote */\nsection { background: url("https://example.test/x.png"); }',
  );
  await assert.rejects(
    renderMarpPreview(request(project, "# X", { themeSet: ["remote.css"] })),
    /cannot use a network resource/,
  );
  await writeFile(
    path.join(project, "escaped.css"),
    '/* @theme escaped */\nsection { background: url("h\\74tps://example.test/x.png"); }',
  );
  await assert.rejects(
    renderMarpPreview(request(project, "# X", { themeSet: ["escaped.css"] })),
    /escaped or commented CSS URL/,
  );
  await writeFile(path.join(project, "local.png"), "not-an-image");
  const local = await renderMarpPreview(request(project, "# X\n\n![image](local.png)"));
  assert.match(local.html, /src="local.png"/);
  const background = await renderMarpPreview(request(project, "# X\n\n![bg](local.png)"));
  assert.match(background.html, /data-marpit-advanced-background/);
  if (process.platform !== "win32") {
    await symlink(path.join(outside, "outside.css"), path.join(project, "linked.css"));
    await assert.rejects(
      renderMarpPreview(request(project, "# X", { themeSet: ["linked.css"] })),
      /resolves outside/,
    );
  }
});

test("resolves local assets from a root-contained entry base directory", async () => {
  const project = await root();
  await writeFile(path.join(project, "asset.png"), "asset");
  await writeFile(path.join(project, "sibling.png"), "asset");
  const slides = path.join(project, "slides");
  await import("node:fs/promises").then(({ mkdir }) => mkdir(slides));
  await writeFile(path.join(slides, "local.png"), "asset");
  await renderMarpPreview(
    request(project, "# X\n\n![local](local.png) ![sibling](../sibling.png)", {
      baseDir: "slides",
    }),
  );
  await assert.rejects(
    renderMarpPreview(request(project, "# X", { baseDir: "../outside" })),
    /base directory is outside/,
  );
});

test("removes remote font imports from built-in themes", async () => {
  const project = await root();
  const result = await renderMarpPreview(request(project, "# 😀", { theme: "gaia" }));
  assert.equal(/@import[^;]*https?:\/\//i.test(result.css), false);
  assert.equal(/url\(\s*["']?https?:\/\//i.test(result.css), false);
  assert.ok(result.warnings.length > 0);
});

test("NDJSON helper preserves string request IDs", async () => {
  const project = await root();
  const helper = path.resolve("scripts/marp-preview.mjs");
  const requests = [
    { id: "request-9007199254740993", root: project, source: "# One" },
    { id: "request-two", root: project, source: "# Two" },
  ];
  const child = spawnSync(process.execPath, [helper], {
    encoding: "utf8",
    input: `${requests.map((value) => JSON.stringify(value)).join("\n")}\n`,
  });
  assert.equal(child.status, 0, child.stderr);
  const responses = child.stdout.trim().split("\n").map((line) => JSON.parse(line));
  assert.deepEqual(responses.map(({ id }) => id), requests.map(({ id }) => id));
  assert.ok(responses.every(({ result }) => result.html.includes("data-marpit-svg")));
});

test("missing Marp Core dependency returns an actionable protocol error", async () => {
  const project = await root();
  const isolated = await root();
  const helper = path.join(isolated, "marp-preview.mjs");
  await writeFile(helper, await readFile(path.resolve("scripts/marp-preview.mjs")));
  const child = spawnSync(process.execPath, [helper], {
    encoding: "utf8",
    input: `${JSON.stringify({ id: "missing-core", root: project, source: "# X" })}\n`,
  });
  assert.equal(child.status, 0, child.stderr);
  const response = JSON.parse(child.stdout.trim());
  assert.equal(response.id, "missing-core");
  assert.match(response.error, /Cannot load @marp-team\/marp-core 4\.3\.1; run npm ci/);
});
