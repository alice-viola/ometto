import { writeFileSync } from 'node:fs';
import { cairnSvg, CAIRN } from '../src/brand/cairn.mjs';

const V = {
  A_current4: { ...CAIRN },
  B_flatter4: { ...CAIRN, widths: [1, 0.78, 0.6, 0.42], heightRatio: 0.48, gap: 0.1 },
  C_small3: { ...CAIRN, widths: [1, 0.72, 0.46], heightRatio: 0.62, gap: 0.13, sway: 0.05 },
  D_small3b: { ...CAIRN, widths: [1, 0.68, 0.4], heightRatio: 0.7, gap: 0.16, sway: 0.05 },
  E_small3c: { ...CAIRN, widths: [1, 0.7, 0.44], heightRatio: 0.66, gap: 0.2, sway: 0.04 },
};
const SIZES = [16, 20, 24, 32, 48];

const block = (ink, paper, label) => `<section style="background:${paper};color:${ink}">
<h2>${label}</h2>
${Object.entries(V)
  .map(
    ([name, spec]) => `<div class="row"><b>${name}</b>${SIZES.map(
      (s) => `<figure>${cairnSvg({ size: s, color: ink, spec })}<figcaption>${s}</figcaption></figure>`,
    ).join('')}${SIZES.map(
      (s) => `<figure>${cairnSvg({ size: s, color: ink, spec, variant: 'outline' })}<figcaption>${s}o</figcaption></figure>`,
    ).join('')}<figure>${cairnSvg({ size: 96, color: ink, spec })}<figcaption>96</figcaption></figure></div>`,
  )
  .join('')}
</section>`;

writeFileSync(process.argv[2], `<!doctype html><meta charset="utf-8"><title>Cairn variants</title>
<style>body{margin:0;font:13px ui-sans-serif,system-ui,sans-serif}
section{padding:22px 26px}h2{font-size:11px;letter-spacing:.06em;text-transform:uppercase;opacity:.55;margin:0 0 14px;font-weight:600}
.row{display:flex;align-items:flex-end;gap:16px;margin-bottom:18px}
b{width:92px;font:600 11px ui-monospace,monospace;opacity:.6}
figure{margin:0;display:flex;flex-direction:column;align-items:center;gap:5px}
figcaption{font-size:9px;opacity:.45}</style>
${block('#15181c', '#f4f4f2', 'light')}
${block('#e9ebee', '#0c0e11', 'dark')}`);
console.log('ok');
