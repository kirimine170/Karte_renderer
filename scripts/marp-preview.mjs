import { realpath, readFile, stat } from "node:fs/promises";
import path from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

let corePromise;
const loadCore = () =>
  (corePromise ??= import("@marp-team/marp-core").catch((error) => {
    throw new Error(
      `Cannot load @marp-team/marp-core 4.3.1; run npm ci beside the preview helper (${error.message})`,
    );
  }));

const inside = (root, candidate) => {
  const relative = path.relative(root, candidate);
  return (
    relative === "" ||
    (relative !== ".." &&
      !relative.startsWith(`..${path.sep}`) &&
      !path.isAbsolute(relative))
  );
};

async function projectRoot(value) {
  if (typeof value !== "string" || value.trim() === "")
    throw new Error("Marp preview root must be a non-empty directory path");
  const root = await realpath(path.resolve(value));
  if (!(await stat(root)).isDirectory())
    throw new Error(`Marp preview root is not a directory: ${value}`);
  return root;
}

async function projectBase(root, value) {
  const candidate = path.resolve(root, value || ".");
  if (!inside(root, candidate))
    throw new Error(`Marp preview base directory is outside the root: ${value}`);
  const base = await realpath(candidate).catch((error) => {
    throw new Error(`Cannot resolve Marp preview base directory ${value}: ${error.message}`);
  });
  if (!inside(root, base) || !(await stat(base)).isDirectory())
    throw new Error(`Marp preview base directory is outside the root or is not a directory: ${value}`);
  return base;
}

const refKind = (value) => {
  const ref = value.trim();
  if (!ref || ref.startsWith("#")) return "empty";
  if (/^data:/i.test(ref)) return "data";
  if (/^(?:https?:)?\/\//i.test(ref)) return "network";
  if (/^[a-z][a-z\d+.-]*:/i.test(ref)) return "scheme";
  return "local";
};

async function nearestExisting(candidate) {
  let current = candidate;
  for (;;) {
    try {
      return await realpath(current);
    } catch (error) {
      if (!["ENOENT", "ENOTDIR"].includes(error?.code)) throw error;
      const parent = path.dirname(current);
      if (parent === current) throw error;
      current = parent;
    }
  }
}

async function validateRef(root, base, value, label) {
  const kind = refKind(value);
  if (kind === "empty" || kind === "data") return;
  if (kind === "network")
    throw new Error(`${label} cannot use a network resource: ${value}`);
  if (kind === "scheme")
    throw new Error(`${label} uses a disallowed URL scheme: ${value}`);
  let decoded;
  try {
    decoded = decodeURIComponent(value.trim().split(/[?#]/, 1)[0]);
  } catch {
    throw new Error(`${label} is not valid URI encoding: ${value}`);
  }
  const candidate = path.resolve(base, decoded.replaceAll("\\", path.sep));
  if (!inside(root, candidate))
    throw new Error(`${label} is outside the Marp preview root: ${value}`);
  if (!inside(root, await nearestExisting(candidate)))
    throw new Error(`${label} resolves outside the Marp preview root: ${value}`);
}

const cssURLs = (css) =>
  [...css.matchAll(/url\(\s*(?:"([^"]*)"|'([^']*)'|([^)]*))\s*\)/gi)].map(
    (match) => (match[1] ?? match[2] ?? match[3] ?? "").trim(),
  );
const cssImports = (css) =>
  [
    ...css.matchAll(
      /@import\s+(?:url\(\s*)?(?:"([^"]*)"|'([^']*)'|([^\s);]+))/gi,
    ),
  ].map((match) => (match[1] ?? match[2] ?? match[3] ?? "").trim());

async function validateCSS(root, base, css, label) {
  if (/@import\s*\/\*/i.test(css))
    throw new Error(`${label} uses a commented CSS import`);
  for (const ref of cssURLs(css)) {
    if (ref.includes("\\") || ref.includes("/*"))
      throw new Error(`${label} uses an escaped or commented CSS URL: ${ref}`);
    await validateRef(root, base, ref, label);
  }
  for (const ref of cssImports(css)) {
    if (ref.includes("\\") || ref.includes("/*"))
      throw new Error(`${label} uses an escaped or commented CSS import: ${ref}`);
    const kind = refKind(ref);
    if (kind !== "local" || /^(?:\.|\/)|[\\/]|\.css$/i.test(ref))
      await validateRef(root, base, ref, label);
  }
}

async function addTheme(marp, root, value, cache) {
  await validateRef(root, root, value, "Marp theme");
  const filename = await realpath(path.resolve(root, value)).catch((error) => {
    throw new Error(`Cannot read Marp theme ${value}: ${error.message}`);
  });
  if (!inside(root, filename))
    throw new Error(`Marp theme resolves outside the Marp preview root: ${value}`);
  if (cache.has(filename)) return cache.get(filename);
  const css = await readFile(filename, "utf8");
  await validateCSS(root, path.dirname(filename), css, `Theme asset in ${value}`);
  try {
    const theme = marp.themeSet.add(css);
    cache.set(filename, theme);
    return theme;
  } catch (error) {
    throw new Error(`Cannot register Marp theme ${value}: ${error.message}`);
  }
}

const themePath = (value) =>
  value.endsWith(".css") || /^(?:\.|\/)/.test(value) || /[\\/]/.test(value);

function overrideTheme(marp, theme) {
  marp.use((markdown) => {
    markdown.core.ruler.after("inline", "karte_preview_theme", (state) => {
      if (state.inlineMode) return;
      const token = new state.Token("marpit_comment", "", 0);
      token.hidden = true;
      token.content = `theme: ${theme}`;
      token.markup = `<!-- theme: ${theme} -->`;
      token.meta = {
        marpitCommentParsed: "karte-preview-theme",
        marpitParsedDirectives: { theme },
      };
      state.tokens.push(token);
    });
  });
}

const attr = (token, name) => token.attrs?.find(([key]) => key === name)?.[1];
function assetsFromTokens(tokens, refs = []) {
  for (const token of tokens ?? []) {
    if (Array.isArray(token)) assetsFromTokens(token, refs);
    else if (token && typeof token === "object") {
      if (token.type === "image" && typeof attr(token, "src") === "string")
        refs.push(attr(token, "src"));
      if (typeof attr(token, "style") === "string")
        refs.push(...cssURLs(attr(token, "style")));
      assetsFromTokens(token.children, refs);
    }
  }
  return refs;
}

const decodeHTML = (value) =>
  value
    .replace(/&quot;/gi, '"')
    .replace(/&apos;|&#39;/gi, "'")
    .replace(/&amp;/gi, "&")
    .replace(/&#x([\da-f]+);/gi, (_, digits) =>
      String.fromCodePoint(Number.parseInt(digits, 16)),
    )
    .replace(/&#(\d+);/g, (_, digits) =>
      String.fromCodePoint(Number.parseInt(digits, 10)),
    );
function attributes(tag) {
  const result = new Map();
  const pattern = /([^\s=/>]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?/g;
  for (const match of tag.matchAll(pattern))
    result.set(
      match[1].toLowerCase(),
      decodeHTML(match[2] ?? match[3] ?? match[4] ?? ""),
    );
  return result;
}

async function validateHTML(root, base, html) {
  const forbidden = new Set(["script", "iframe", "object", "embed", "link"]);
  const resources = new Map([
    ["img", ["src", "srcset"]],
    ["image", ["href", "xlink:href"]],
    ["use", ["href", "xlink:href"]],
    ["feimage", ["href", "xlink:href"]],
    ["video", ["src", "poster"]],
    ["audio", ["src"]],
    ["source", ["src", "srcset"]],
    ["track", ["src"]],
    ["input", ["src"]],
  ]);
  for (const match of html.matchAll(/<([a-z][\w:-]*)\b[^>]*>/gi)) {
    const element = match[1].toLowerCase();
    if (forbidden.has(element))
      throw new Error(`${element} elements are not allowed in Marp preview HTML`);
    const attrs = attributes(match[0]);
    for (const [name, value] of attrs) {
      if (name.startsWith("on"))
        throw new Error(`HTML event handler ${name} is not allowed in Marp preview`);
      if (/^javascript:/i.test(value.trim()))
        throw new Error("javascript URLs are not allowed in Marp preview");
    }
    for (const ref of cssURLs(attrs.get("style") ?? ""))
      await validateRef(root, base, ref, "Marp HTML style asset");
    if (attrs.has("background"))
      await validateRef(root, base, attrs.get("background"), `Marp HTML ${element}.background`);
    if (element === "meta" && /^refresh$/i.test(attrs.get("http-equiv") ?? ""))
      throw new Error("meta refresh is not allowed in Marp preview HTML");
    for (const name of resources.get(element) ?? []) {
      const value = attrs.get(name);
      if (!value) continue;
      const refs = name === "srcset" ? value.split(",").map((v) => v.trim().split(/\s+/, 1)[0]) : [value];
      for (const ref of refs)
        await validateRef(root, base, ref, `Marp HTML ${element}.${name}`);
    }
  }
}

function safeCSS(css) {
  const warnings = [];
  const result = css.replace(/@import\s+[^;]*;/gi, (statement) => {
    const remote = cssImports(statement).filter((ref) => refKind(ref) === "network");
    if (!remote.length) return statement;
    warnings.push(`Disabled network CSS import: ${remote.join(", ")}`);
    return "";
  });
  for (const ref of [...cssURLs(result), ...cssImports(result)]) {
    if (ref.includes("\\") || ref.includes("/*"))
      throw new Error(`Generated Marp CSS contains an escaped resource: ${ref}`);
    if (refKind(ref) === "network")
      throw new Error(`Generated Marp CSS contains a network resource: ${ref}`);
  }
  return { css: result, warnings };
}

export async function renderMarpPreview(request) {
  if (!request || typeof request !== "object")
    throw new Error("Marp preview request must be an object");
  if (typeof request.source !== "string")
    throw new Error("Marp preview source must be a string");
  if (request.themeSet !== undefined && !Array.isArray(request.themeSet))
    throw new Error("Marp preview themeSet must be an array");
  const root = await projectRoot(request.root);
  const base = await projectBase(root, request.baseDir);
  const { Marp } = await loadCore();
  const marp = new Marp({
    emoji: { shortcode: false, unicode: false },
    html: request.trustedHTML === true,
    script: false,
  });
  const themes = new Map();
  for (const value of request.themeSet ?? []) {
    if (typeof value !== "string" || !value.trim())
      throw new Error("Marp preview theme-set entries must be non-empty paths");
    await addTheme(marp, root, value, themes);
  }
  let selected = request.theme?.trim();
  if (selected) {
    if (themePath(selected)) selected = (await addTheme(marp, root, selected, themes)).name;
    if (!marp.themeSet.has(selected))
      throw new Error(`Marp preview theme is not registered: ${selected}`);
    overrideTheme(marp, selected);
  }
  const rendered = marp.render(request.source);
  for (const ref of assetsFromTokens(marp.lastSlideTokens))
    await validateRef(root, base, ref, "Marp Markdown asset");
  await validateHTML(root, base, rendered.html);
  const sanitized = safeCSS(rendered.css);
  return {
    html: rendered.html,
    css: sanitized.css,
    comments: rendered.comments ?? [],
    warnings: sanitized.warnings,
  };
}

const message = (error) => (error instanceof Error ? error.message : String(error));
export async function serve(input = process.stdin, output = process.stdout) {
  const lines = createInterface({ input, crlfDelay: Infinity, terminal: false });
  for await (const line of lines) {
    if (!line.trim()) continue;
    let request;
    try {
      request = JSON.parse(line);
    } catch (error) {
      output.write(`${JSON.stringify({ id: "", error: `Invalid request JSON: ${message(error)}` })}\n`);
      continue;
    }
    const id = typeof request.id === "string" ? request.id : "";
    if (!id) {
      output.write(`${JSON.stringify({ id, error: "Marp preview request id must be a non-empty string" })}\n`);
      continue;
    }
    try {
      output.write(`${JSON.stringify({ id, result: await renderMarpPreview(request) })}\n`);
    } catch (error) {
      output.write(`${JSON.stringify({ id, error: message(error) })}\n`);
    }
  }
}

if (process.argv[1]) {
  const invoked = await realpath(path.resolve(process.argv[1])).catch(() => "");
  const current = await realpath(fileURLToPath(import.meta.url));
  if (invoked === current)
    serve().catch((error) => {
      process.stderr.write(`Marp preview helper failed: ${message(error)}\n`);
      process.exitCode = 1;
    });
}
