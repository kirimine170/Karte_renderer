#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
source_package="${repo_root}/examples/karte-format-report"
build_root="${KARTE_REPORT_BUILD_DIR:-${repo_root}/build/report-format}"
mkdir -p "${build_root}"
run_root="$(mktemp -d "${build_root}/run.XXXXXX")"

cleanup() {
  if [[ "${KARTE_REPORT_KEEP_OUTPUTS:-0}" != "1" ]]; then
    rm -rf -- "${run_root}"
  fi
}
trap cleanup EXIT

package_root="${run_root}/karte-format-report"
mkdir -p "${package_root}"
cp -R "${source_package}/." "${package_root}/"
mkdir -p "${package_root}/themes/default" "${run_root}/output"
cp "${package_root}/markdown/layout.html" "${package_root}/themes/default/layout.html"
awk 'FNR == 1 && NR != 1 { print "" } { print }' \
  "${package_root}/markdown/base.css" \
  "${package_root}/markdown/print.css" \
  > "${package_root}/report.css"

expected_values="$(node -e '
  const expected = require(process.argv[1]);
  process.stdout.write(`${expected.document.pages} ${expected.marp.slides} ${expected.marp.theme}`);
' "${package_root}/expected.json")"
read -r document_pages marp_slides marp_theme <<< "${expected_values}"

browser="${KARTE_REPORT_CHROMIUM:-}"
if [[ -z "${browser}" ]]; then
  browser="$(command -v google-chrome-stable || command -v google-chrome || command -v chromium || command -v brave-browser || true)"
fi
if [[ -z "${browser}" && -x "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" ]]; then
  browser="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
fi
if [[ -z "${browser}" ]]; then
  echo "Chrome/Chromium is required for the report format smoke test" >&2
  exit 1
fi

marp_binary="${MARP_BINARY:-${repo_root}/node_modules/.bin/marp}"
if [[ ! -x "${marp_binary}" ]]; then
  echo "Marp CLI is required; run npm ci or set MARP_BINARY" >&2
  exit 1
fi
for tool in pdfinfo pdffonts pdftotext unzip; do
  command -v "${tool}" >/dev/null || { echo "${tool} is required for the report format smoke test" >&2; exit 1; }
done

renderer="${run_root}/karte-renderer"
go build -o "${renderer}" ./cmd/karte-renderer

document_input="${package_root}/fixtures/activity-report.md"
document_html="${run_root}/output/activity-report.html"
document_pdf="${run_root}/output/activity-report.pdf"
document_preflight="${run_root}/output/activity-report.preflight.json"
"${renderer}" \
  --root "${package_root}" \
  --css "${package_root}/report.css" \
  "${document_input}" "${document_html}"
"${renderer}" \
  --root "${package_root}" \
  --css "${package_root}/report.css" \
  --pdf-engine chromium \
  --pdf-binary "${browser}" \
  --expected-pages "${document_pages}" \
  --preflight-report "${document_preflight}" \
  --pdfinfo-binary "$(command -v pdfinfo)" \
  --pdffonts-binary "$(command -v pdffonts)" \
  --pdftotext-binary "$(command -v pdftotext)" \
  "${document_input}" "${document_pdf}"

marp_input="${package_root}/fixtures/activity-report.marp.md"
marp_html="${package_root}/fixtures/activity-report.marp.html"
marp_pdf="${package_root}/fixtures/activity-report.marp.pdf"
marp_pptx="${package_root}/fixtures/activity-report.pptx"
marp_args=(
  --root "${package_root}"
  --marp-binary "${marp_binary}"
  --theme "${marp_theme}"
  --theme-set "${package_root}/marp/karte-activity-report.css"
  --html
  --allow-local-files
  --browser-path "${browser}"
)
"${renderer}" "${marp_args[@]}" "${marp_input}" "${marp_html}"
"${renderer}" "${marp_args[@]}" "${marp_input}" "${marp_pdf}"
"${renderer}" "${marp_args[@]}" "${marp_input}" "${marp_pptx}"

grep -q '"passed": true' "${document_preflight}"
test "$(pdfinfo "${document_pdf}" | awk '/^Pages:/ { print $2 }')" = "${document_pages}"
test "$(pdfinfo "${marp_pdf}" | awk '/^Pages:/ { print $2 }')" = "${marp_slides}"
test "$(unzip -Z1 "${marp_pptx}" | grep -Ec '^ppt/slides/slide[0-9]+\.xml$')" = "${marp_slides}"

node -e '
  const fs = require("node:fs");
  const path = require("node:path");
  const htmlFile = process.argv[1];
  const expectedSlides = Number(process.argv[2]);
  const html = fs.readFileSync(htmlFile, "utf8");
  const slides = (html.match(/<section\b/g) || []).length;
  if (slides !== expectedSlides) throw new Error(`Marp HTML has ${slides} slides; expected ${expectedSlides}`);
  const references = [...html.matchAll(/(?:src=["'\'' ]*|url\(["'\'' ]*)(\.\.\/assets\/progress\.svg)/g)].map((match) => match[1]);
  if (references.length < 2) throw new Error("Marp HTML is missing the shared progress asset references");
  for (const reference of references) fs.statSync(path.resolve(path.dirname(htmlFile), reference));
' "${marp_html}" "${marp_slides}"

echo "report fixture smoke passed: ${document_pages}-page document PDF, ${marp_slides}-slide Marp HTML/PDF/PPTX"
if [[ "${KARTE_REPORT_KEEP_OUTPUTS:-0}" == "1" ]]; then
  echo "report fixture evidence: ${run_root}"
fi
