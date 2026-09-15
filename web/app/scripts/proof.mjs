/** The proof sheet: the mark at real sizes, in both themes, plus the exports. */
import { writeFileSync } from 'node:fs';
import { cairnSvg, specFor, CAIRN, CAIRN_SMALL } from '../src/brand/cairn.mjs';

const SIZES = [16, 24, 32, 64, 256];
const OUT =
  process.argv[2] ??
  '/private/tmp/claude-502/-Users-alice-Work-queen/bcc5eab1-da8d-416d-bc4f-0e3823053063/scratchpad/ometto-mark.html';

const mark = (s, ink, variant = 'filled') =>
  cairnSvg({ size: s, color: ink, variant, spec: specFor(s) });

const theme = (ink, paper, label, muted) => `
<section style="background:${paper};color:${ink}">
  <h2>${label}</h2>

  <div class="row">
    ${SIZES.map(
      (s) =>
        `<figure>${mark(s, ink)}<figcaption>${s} px${s < 24 ? ' · 3 stones' : ''}</figcaption></figure>`,
    ).join('')}
  </div>

  <div class="row">
    ${SIZES.map(
      (s) =>
        `<figure>${mark(s, ink, 'outline')}<figcaption>${s} px outline</figcaption></figure>`,
    ).join('')}
  </div>

  <h2>In place</h2>
  <div class="row header">
    <div class="hdr" style="border-color:${muted}33">
      ${mark(22, ink)}
      <div class="words">
        <div class="name">Ometto</div>
        <div class="tag" style="color:${muted}">Trentino-Alto Adige</div>
      </div>
    </div>
    <div class="tabstrip" style="border-color:${muted}33">
      ${mark(16, ink)}<span style="font-size:12px">Ometto — routes in Trentino-Alto Adige</span>
    </div>
  </div>

  <h2>Magnified &times;8 — the test that set the numbers</h2>
  <div class="row">
    ${[16, 20, 24, 32]
      .map(
        (s) =>
          `<figure><canvas data-size="${s}" data-ink="${ink}"></canvas><figcaption>${s} px</figcaption></figure>`,
      )
      .join('')}
  </div>
</section>`;

writeFileSync(
  OUT,
  `<!doctype html><meta charset="utf-8"><title>Ometto — the mark</title>
<style>
 body{margin:0;font:13px/1.5 ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
 section{padding:28px 32px}
 h2{font-size:11px;letter-spacing:.07em;text-transform:uppercase;opacity:.5;margin:0 0 18px;font-weight:600}
 .row{display:flex;align-items:flex-end;gap:30px;margin-bottom:30px;flex-wrap:wrap}
 figure{margin:0;display:flex;flex-direction:column;align-items:center;gap:8px}
 figcaption{font-size:10px;opacity:.5;font-variant-numeric:tabular-nums}
 .hdr{display:flex;align-items:center;gap:9px;border:1px solid;border-radius:11px;padding:11px 14px}
 .words{line-height:1.15}
 .name{font-size:15px;font-weight:500;letter-spacing:-.005em}
 .tag{font-size:11.5px}
 .tabstrip{display:flex;align-items:center;gap:7px;border:1px solid;border-radius:8px;padding:7px 11px}
 canvas{image-rendering:pixelated}
 pre{font:11px/1.6 ui-monospace,SFMono-Regular,monospace;opacity:.65;margin:0;white-space:pre-wrap}
 .files{display:flex;gap:26px;flex-wrap:wrap;align-items:flex-end}
 .files img{image-rendering:auto}
</style>

${theme('#15181c', '#f4f4f2', 'Light — ink on paper', '#676d75')}
${theme('#e9ebee', '#0c0e11', 'Dark — paper on ink', '#949aa3')}

<section style="background:#f4f4f2;color:#15181c">
  <h2>Exported files (as the browser loads them)</h2>
  <div class="files">
    <figure><img src="/app-public/favicon.svg" width="64" height="64"><figcaption>favicon.svg</figcaption></figure>
    <figure><img src="/app-public/favicon.ico" width="48" height="48"><figcaption>favicon.ico 48</figcaption></figure>
    <figure><img src="/app-public/favicon.ico" width="32" height="32"><figcaption>ico 32</figcaption></figure>
    <figure><img src="/app-public/favicon.ico" width="16" height="16"><figcaption>ico 16</figcaption></figure>
    <figure><img src="/app-public/apple-touch-icon.png" width="90" height="90"><figcaption>apple-touch 180</figcaption></figure>
    <figure><img src="/app-public/icon-192.png" width="96" height="96"><figcaption>icon-192</figcaption></figure>
    <figure><img src="/app-public/icon-512.png" width="128" height="128"><figcaption>icon-512</figcaption></figure>
  </div>
  <div style="margin-top:26px">
    <img src="/app-public/og.png" width="600" height="315" style="border:1px solid #ddd">
    <div style="font-size:10px;opacity:.5;margin-top:6px">og.png — 1200&times;630</div>
  </div>
</section>

<section style="background:#f4f4f2;color:#15181c">
  <h2>Spec</h2>
  <pre>CAIRN        ${JSON.stringify(CAIRN, null, 1)}

CAIRN_SMALL  ${JSON.stringify(CAIRN_SMALL, null, 1)}</pre>
</section>

<script>
const svgs = ${JSON.stringify(
    Object.fromEntries(
      ['#15181c', '#e9ebee'].map((ink) => [
        ink,
        Object.fromEntries([16, 20, 24, 32].map((s) => [s, mark(s, ink)])),
      ]),
    ),
  )};
for (const c of document.querySelectorAll('canvas')) {
  const s = +c.dataset.size, ink = c.dataset.ink;
  c.width = s*8; c.height = s*8; c.style.width=(s*8)+'px'; c.style.height=(s*8)+'px';
  const ctx = c.getContext('2d');
  const img = new Image();
  img.onload = () => {
    const o = document.createElement('canvas'); o.width=s; o.height=s;
    o.getContext('2d').drawImage(img,0,0,s,s);
    ctx.imageSmoothingEnabled = false;
    ctx.drawImage(o,0,0,s,s,0,0,s*8,s*8);
  };
  img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svgs[ink][s]);
}
</script>`,
);
console.log('wrote', OUT);
