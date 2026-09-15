import { writeFileSync } from 'node:fs';
import { cairnSvg, CAIRN } from '../src/brand/cairn.mjs';

const V = {
  A_current4: { ...CAIRN },
  B_flatter4: { ...CAIRN, widths: [1, 0.78, 0.6, 0.42], heightRatio: 0.48, gap: 0.1 },
  C_small3: { ...CAIRN, widths: [1, 0.72, 0.46], heightRatio: 0.62, gap: 0.13, sway: 0.05 },
  E_small3c: { ...CAIRN, widths: [1, 0.7, 0.44], heightRatio: 0.66, gap: 0.2, sway: 0.04 },
};
const SIZES = [16, 20, 24, 32];
const data = Object.fromEntries(
  Object.entries(V).map(([k, spec]) => [
    k,
    Object.fromEntries(SIZES.map((s) => [s, cairnSvg({ size: s, color: '#15181c', spec })])),
  ]),
);

writeFileSync(process.argv[2], `<!doctype html><meta charset="utf-8"><title>Cairn at small sizes, magnified</title>
<style>body{margin:0;background:#f4f4f2;color:#15181c;font:13px ui-sans-serif,system-ui,sans-serif;padding:20px}
table{border-collapse:collapse}td,th{padding:8px 10px;text-align:center;font-size:10px;opacity:.85}
th{font:600 10px ui-monospace,monospace;letter-spacing:.04em}
canvas{image-rendering:pixelated;border:1px solid #ddd;background:#fff}</style>
<table id="t"></table>
<script>
const data = ${JSON.stringify(data)};
const sizes = ${JSON.stringify(SIZES)};
const t = document.getElementById('t');
const head = document.createElement('tr');
head.innerHTML = '<th></th>' + sizes.map(s => '<th>'+s+' px &times;8</th>').join('') + sizes.map(s => '<th>'+s+' actual</th>').join('');
t.appendChild(head);
for (const [name, bySize] of Object.entries(data)) {
  const tr = document.createElement('tr');
  const th = document.createElement('th'); th.textContent = name; tr.appendChild(th);
  for (const s of sizes) {
    const td = document.createElement('td');
    const c = document.createElement('canvas');
    c.width = s*8; c.height = s*8; c.style.width=(s*8)+'px'; c.style.height=(s*8)+'px';
    const ctx = c.getContext('2d');
    const img = new Image();
    img.onload = () => { const o=document.createElement('canvas'); o.width=s;o.height=s;
      o.getContext('2d').drawImage(img,0,0,s,s);
      ctx.imageSmoothingEnabled=false; ctx.drawImage(o,0,0,s,s,0,0,s*8,s*8); };
    img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(bySize[s]);
    td.appendChild(c); tr.appendChild(td);
  }
  for (const s of sizes) {
    const td = document.createElement('td');
    td.innerHTML = bySize[s];
    tr.appendChild(td);
  }
  t.appendChild(tr);
}
</script>`);
console.log('ok');
