import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { cp, mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parse as parseYAML } from "yaml";

const root = path.resolve("examples/karte-format-report");

test("activity report Marp fixture renders the expected themed slides", async () => {
  const expected = JSON.parse(await readFile(path.join(root, "expected.json"), "utf8"));
  const source = await readFile(path.join(root, "fixtures/activity-report.marp.md"), "utf8");
  const frontMatter = parseFrontMatter(source);
  assert.equal(frontMatter.marp, true);
  assert.equal(frontMatter.title, expected.marp.title);
  assert.equal(frontMatter.theme, expected.marp.theme);
  await stat(path.join(root, expected.marp.asset));

  const build = await mkdtemp(path.join(tmpdir(), "karte-report-marp-"));
  try {
    const packageRoot = path.join(build, "karte-format-report");
    await cp(root, packageRoot, { recursive: true });
    const input = path.join(packageRoot, "fixtures/activity-report.marp.md");
    const theme = path.join(packageRoot, "marp/karte-activity-report.css");
    const output = path.join(packageRoot, "fixtures/activity-report.marp.html");
    const marpCLI = path.resolve("node_modules/@marp-team/marp-cli/marp-cli.js");
    const result = spawnSync(process.execPath, [marpCLI, input, "--output", output, "--theme-set", theme, "--theme", expected.marp.theme, "--html", "--allow-local-files"], { encoding: "utf8" });
    assert.equal(result.status, 0, result.stderr || result.stdout);
    const html = await readFile(output, "utf8");
    assert.equal((html.match(/<section\b/g) ?? []).length, expected.marp.slides);
    assert.match(html, /--karte-accent:\s*#6f42c1/);
    assert.match(html, /section table\{display:table;width:100%/);
    assert.match(html, /主要マイルストーンの92%を完了/);
    const progressReferences = [...html.matchAll(/(?:src=["']|url\(["']?)(\.\.\/assets\/progress\.svg)/g)].map((match) => match[1]);
    assert.ok(progressReferences.length >= 2, "expected slide image and theme background references");
    for (const reference of progressReferences) {
      await stat(fileURLToPath(new URL(reference, pathToFileURL(output))));
    }
  } finally {
    await rm(build, { recursive: true, force: true });
  }
});

function parseFrontMatter(source) {
  const match = source.match(/^---\s*\r?\n([\s\S]*?)\r?\n---\s*(?:\r?\n|$)/);
  assert.ok(match, "Marp fixture must have YAML front matter");
  return parseYAML(match[1]);
}
