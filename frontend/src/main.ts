import {Events} from "@wailsio/runtime";
import {OCRService} from "../bindings/github.com/automationd/screenpear/services";
import type {Analysis, MaskRequest} from "../bindings/github.com/automationd/screenpear/services";
import {ShellService} from "../bindings/github.com/automationd/screenpear";
import type {Rule} from "../bindings/github.com/automationd/screenpear/internal/mask";

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id)! as T;

const openBtn = $<HTMLButtonElement>("open");
const analyzeBtn = $<HTMLButtonElement>("analyze");
const maskBtn = $<HTMLButtonElement>("mask");
const statusEl = $<HTMLSpanElement>("status");
const dropzone = $<HTMLDivElement>("dropzone");
const canvasWrap = $<HTMLDivElement>("canvasWrap");
const preview = $<HTMLImageElement>("preview");
const overlay = $<SVGSVGElement & HTMLElement>("overlay");
const linesEl = $<HTMLUListElement>("lines");
const lineCount = $<HTMLSpanElement>("lineCount");
const rulesEl = $<HTMLTextAreaElement>("rules");
const resultCard = $<HTMLElement>("resultCard");
const resultPath = $<HTMLParagraphElement>("resultPath");
const replacementsEl = $<HTMLUListElement>("replacements");
const revealBtn = $<HTMLButtonElement>("reveal");
const substituteAllEl = $<HTMLInputElement>("substituteAll");
const inpaintEl = $<HTMLInputElement>("inpaint");

let currentPath = "";
let analysis: Analysis | null = null;
let resultOutput = "";
const excluded = new Set<number>();

function setStatus(text: string, kind: "" | "busy" | "error" = "") {
    statusEl.textContent = text;
    statusEl.dataset.kind = kind;
}

function parseRules(text: string): Rule[] {
    return text.split("\n")
        .map(l => l.trim())
        .filter(l => l && !l.startsWith("#") && l.includes("="))
        .map(l => {
            const i = l.indexOf("=");
            return {from: l.slice(0, i).trim(), to: l.slice(i + 1).trim()} as Rule;
        })
        .filter(r => r.from);
}

// Which lines a rule would change, so the list can highlight them before
// masking. Mirrors mask.Rewrite: longest `from` first, literal, case-sensitive.
function wouldChange(text: string, rules: Rule[]): boolean {
    if (substituteAllEl.checked && /[\p{L}\p{N}]/u.test(text)) return true;
    const ordered = [...rules].sort((a, b) => b.from.length - a.from.length);
    let out = text;
    for (const r of ordered) out = out.split(r.from).join(r.to);
    return out !== text;
}

async function loadImage(path: string) {
    currentPath = path;
    analysis = null;
    excluded.clear();
    resultCard.hidden = true;
    preview.src = "/api/image?path=" + encodeURIComponent(path) + "&t=" + Date.now();
    dropzone.classList.add("hidden");
    canvasWrap.classList.remove("hidden");
    renderLines();
    analyzeBtn.disabled = false;
    maskBtn.disabled = true;
    setStatus(path.split("/").pop() ?? path);
    await analyze();
}

async function analyze() {
    if (!currentPath) return;
    setStatus("Detecting text…", "busy");
    analyzeBtn.disabled = true;
    try {
        analysis = await OCRService.Analyze(currentPath);
        // Icons read as characters start deselected; a click re-enables them.
        for (const l of analysis?.lines ?? []) if (l.kind === "symbol") excluded.add(l.id);
        setStatus(`${analysis?.lines?.length ?? 0} lines in ${analysis?.elapsedMs ?? 0} ms`);
        maskBtn.disabled = false;
    } catch (err) {
        setStatus(String(err), "error");
    } finally {
        analyzeBtn.disabled = false;
        renderLines();
    }
}

function renderLines() {
    linesEl.replaceChildren();
    overlay.replaceChildren();
    const lines = analysis?.lines ?? [];
    lineCount.textContent = `${lines.length} lines`;
    if (!analysis) return;

    overlay.setAttribute("viewBox", `0 0 ${analysis.width} ${analysis.height}`);
    const rules = parseRules(rulesEl.value);
    for (const line of lines) {
        const hit = wouldChange(line.text, rules);
        const on = !excluded.has(line.id);

        const rect = document.createElementNS("http://www.w3.org/2000/svg", "rect");
        rect.setAttribute("x", String(line.x0));
        rect.setAttribute("y", String(line.y0));
        rect.setAttribute("width", String(line.x1 - line.x0));
        rect.setAttribute("height", String(line.y1 - line.y0));
        rect.setAttribute("class", "box" + (hit ? " box-hit" : "") + (on ? "" : " box-off"));
        rect.addEventListener("click", () => toggleLine(line.id));
        overlay.appendChild(rect);

        const li = document.createElement("li");
        li.className = "line" + (hit ? " line-hit" : "") + (on ? "" : " line-off");
        const cb = document.createElement("input");
        cb.type = "checkbox";
        cb.checked = on;
        cb.addEventListener("change", () => toggleLine(line.id));
        const text = document.createElement("span");
        text.className = "line-text";
        text.textContent = line.kind === "symbol" ? `${line.text}  (symbol)` : line.text;
        const score = document.createElement("span");
        score.className = "muted mono";
        score.textContent = line.score.toFixed(2);
        li.append(cb, text, score);
        li.addEventListener("mouseenter", () => rect.classList.add("box-hover"));
        li.addEventListener("mouseleave", () => rect.classList.remove("box-hover"));
        linesEl.appendChild(li);
    }
}

function toggleLine(id: number) {
    if (excluded.has(id)) excluded.delete(id); else excluded.add(id);
    renderLines();
}

async function mask() {
    if (!analysis) return;
    setStatus("Masking…", "busy");
    maskBtn.disabled = true;
    try {
        const lines = analysis.lines ?? [];
        const lineIds = lines.map(l => l.id).filter(id => !excluded.has(id));
        const req: MaskRequest = {
            path: currentPath,
            rules: parseRules(rulesEl.value),
            lines,
            lineIds: lineIds.length === lines.length ? [] : lineIds,
            substituteAll: substituteAllEl.checked,
            flatBackground: !inpaintEl.checked,
        };
        const res = await OCRService.Mask(req);
        if (!res) throw new Error("mask returned nothing");
        resultOutput = res.output;
        resultPath.textContent = res.output;
        replacementsEl.replaceChildren(...(res?.replaced ?? []).map(r => {
            const li = document.createElement("li");
            li.className = "line";
            li.innerHTML = `<span class="line-text"><s class="muted">${escape(r.from)}</s> → ${escape(r.to)}</span>`;
            return li;
        }));
        resultCard.hidden = false;
        preview.src = "/api/image?path=" + encodeURIComponent(res.output) + "&t=" + Date.now();
        setStatus(`${res.replaced?.length ?? 0} replacements → ${res.output.split("/").pop()}`);
    } catch (err) {
        setStatus(String(err), "error");
    } finally {
        maskBtn.disabled = false;
    }
}

function escape(s: string): string {
    return s.replace(/[&<>"]/g, c => ({"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;"}[c]!));
}

openBtn.addEventListener("click", async () => {
    try {
        const path = await ShellService.PickImage();
        if (path) await loadImage(path);
    } catch (err) {
        setStatus(String(err), "error");
    }
});
analyzeBtn.addEventListener("click", analyze);
maskBtn.addEventListener("click", mask);
revealBtn.addEventListener("click", () => resultOutput && ShellService.Reveal(resultOutput));
rulesEl.addEventListener("input", renderLines);
substituteAllEl.addEventListener("change", renderLines);

// Drag and drop: Wails resolves dropped files to absolute paths on the Go
// side; the DOM event only carries names, so we go through the bindings.
document.addEventListener("dragover", e => { e.preventDefault(); dropzone.classList.add("drag"); });
document.addEventListener("dragleave", () => dropzone.classList.remove("drag"));
document.addEventListener("drop", e => { e.preventDefault(); dropzone.classList.remove("drag"); });

// Remote open: the tray and POST /api/open push a path in; same path as a drop.
Events.On("open", (e: {data: string}) => { if (e.data) void loadImage(e.data); });

// Keyboard: Ctrl/Cmd+O open, Ctrl/Cmd+Enter mask.
document.addEventListener("keydown", e => {
    if (!(e.ctrlKey || e.metaKey)) return;
    if (e.key === "o") { e.preventDefault(); openBtn.click(); }
    if (e.key === "Enter" && !maskBtn.disabled) { e.preventDefault(); void mask(); }
});

(async () => {
    rulesEl.value = ((await OCRService.DefaultRules()) ?? []).map(r => `${r.from}=${r.to}`).join("\n");
    try {
        await OCRService.Ready();
        setStatus("Ready");
    } catch (err) {
        setStatus("OCR unavailable: " + String(err), "error");
    }
})();
