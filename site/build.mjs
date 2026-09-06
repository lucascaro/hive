// Renders site/src/*.html templates into site/dist/.
// Data: features.json (living feature list, also rendered by the app's
// What's New modal) + ../CHANGELOG.md (generated).
import { readFileSync, writeFileSync, mkdirSync, cpSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { marked } from "marked";

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, "src");
const dist = join(here, "dist");

const esc = (s) => s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);

// --- features ---
const features = JSON.parse(readFileSync(join(here, "features.json"), "utf8"));
// `since` is what the app's What's New modal groups by, so a shipped entry
// without one silently vanishes from a version bucket there while looking
// fine here. Fail the build instead. (cmd/hivegui/frontend/test/unit/whats-new.test.ts
// asserts the same thing, because CI never builds site/ on a PR.)
for (const f of features) {
  if (f.status === "shipped" && !/^\d+\.\d+\.\d+/.test(f.since ?? "")) {
    throw new Error(`features.json: shipped feature ${JSON.stringify(f.title)} needs a semver \`since\``);
  }
}
const card = (f) =>
  `<div class="feature${f.highlight ? " feature-hi" : ""}${f.status === "planned" ? " feature-planned" : ""}">
    <h3>${esc(f.title)}</h3>
    ${f.blurb ? `<p>${esc(f.blurb)}</p>` : ""}
  </div>`;
const shipped = features.filter((f) => f.status === "shipped").map(card).join("\n");
const planned = features.filter((f) => f.status === "planned")
  .map((f) => `<li${f.blurb ? ` title="${esc(f.blurb)}"` : ""}>${esc(f.title)}</li>`).join("\n");

// --- changelog ---
const md = readFileSync(join(here, "..", "CHANGELOG.md"), "utf8");
const body = md.slice(md.indexOf("## ["));
const sections = body.split(/^(?=## \[)/m);
const versionOf = (s) => s.match(/^## \[([^\]]+)\]/)[1];
const latestRelease = sections.map(versionOf).find((v) => v !== "Unreleased");
marked.use({ mangle: false, headerIds: false });
const renderSection = (s) => {
  const v = versionOf(s);
  const date = (s.match(/^## \[[^\]]+\] — (\S+)/) || [])[1] || "";
  const rest = s.replace(/^## .*\n/, "");
  return `<article class="release" id="v${esc(v)}">
    <header><h2>${esc(v)}</h2>${date ? `<time>${esc(date)}</time>` : ""}</header>
    ${marked.parse(rest)}
  </article>`;
};
const changelogFull = sections.map(renderSection).join("\n");
// Landing page: latest release only, first few bullets, link to the rest.
const latest = sections.find((s) => versionOf(s) === latestRelease);
const renderRecent = (s) => {
  let n = 0;
  const html = renderSection(s).replace(/<li>[\s\S]*?<\/li>/g, (li) => (++n <= 4 ? li : ""))
    .replace(/<h3>[^<]*<\/h3>\s*<ul>\s*<\/ul>/g, "");
  return html.replace(/<\/article>/, `<p class="more"><a href="./changelog.html#v${esc(latestRelease)}">Everything in ${esc(latestRelease)} →</a></p></article>`);
};
const changelogRecent = renderRecent(latest);

// --- templates ---
const vars = {
  features_shipped: shipped,
  features_planned: planned,
  changelog_recent: changelogRecent,
  changelog_full: changelogFull,
  version: latestRelease,
  year: String(new Date().getFullYear()),
};
const render = (tpl) => tpl.replace(/\{\{\s*(\w+)\s*\}\}/g, (_, k) => {
  if (!(k in vars)) throw new Error(`unknown template var ${k}`);
  return vars[k];
});

rmSync(dist, { recursive: true, force: true });
mkdirSync(dist, { recursive: true });
cpSync(src, dist, { recursive: true });
for (const page of ["index.html", "changelog.html"]) {
  writeFileSync(join(dist, page), render(readFileSync(join(src, page), "utf8")));
}
writeFileSync(join(dist, ".nojekyll"), "");
console.log(`built site/dist — v${latestRelease}, ${features.length} features, ${sections.length} changelog sections`);
