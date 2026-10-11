# Rewriting text in screenshots: fonts, weight, slant, OS, models

Research note, 2026-10-10. Goal: make masked lines indistinguishable from the original.

## Font identification models (open source)

| Model | Backbone | Fonts | Notes |
|---|---|---|---|
| [dchen0/font_classifier_v4](https://huggingface.co/dchen0/font_classifier_v4) | DINOv2-base + LoRA | 394 (Google Fonts) | [paper](https://arxiv.org/pdf/2602.13889), ~86% community / 99% benchmark; preprocessing built in |
| [storia/font-classify-onnx](https://github.com/Storia-AI/font-classify) | small CNN, ONNX | ~3000 | ONNX: could run through the same runtime path as PP-OCR |
| [gaborcselle/font-identifier](https://huggingface.co/gaborcselle/font-identifier) | ResNet-18 | 48 | confuses Helvetica/Arial, as a human would |
| [YuzuMarker.FontDetection](https://github.com/JeffersonQin/YuzuMarker.FontDetection) | ResNet | CJK | also extracts style |

All trained on Google Fonts: none knows SF Pro or Segoe UI, which is most of what screenshots contain. Use them for
*family class* (grotesque / humanist / mono / serif), not the exact face.

## Render-and-compare (recommended for screenshots)

[FontsChecker](https://github.com/DannyRuchtie/FontsChecker) pipeline: crop → foreground → per-glyph alignment →
render the OCR transcription in each candidate (family × weight × size) → pixel overlap with edge tolerance →
mean/min per-letter similarity + a spacing score. Deterministic, no model, candidate set is small for UI (10–20
faces), fully on-device. Weight and optical size are separate candidates.

## Bold / italic heuristics (no model)

- Bold: median stroke width from the distance transform of the binarized glyphs, normalised by x-height; compare
  against neighbouring lines on the same screenshot, never an absolute threshold
  ([Chaudhuri & Garain run-length](http://mile.ee.iisc.ac.in/publications/softCopy/DocumentAnalysis/Sai_NCVPRIPG2013.pdf),
  [Bloomberg iterative thinning](https://image-ppubs.uspto.gov/dirsearch-public/print/downloadPdf/5402504)).
- Italic: shear angle from the dominant near-vertical stroke direction (Hough / projection profile sweep over
  -5..20°); italic shows fewer vertical and more diagonal strokes
  ([Devanagari VSLS/CDS features](https://www.researchgate.net/publication/258650817_Detection_of_Bold_and_Italic_Character_in_Devanagari_Script)).
- Pitfall: a bold thin face is still thinner than a regular heavy face
  ([patent 12260659](https://image-ppubs.uspto.gov/dirsearch-public/print/downloadPdf/12260659)); page-relative wins.

## OS detection → font prior

Cheap signals: traffic-light buttons / 28 px title bar / menu bar → macOS (SF Pro, Helvetica Neue in old apps);
Windows caption buttons, taskbar → Segoe UI (use metric-compatible Selawik); GNOME header bars → Cantarell/Adwaita
Sans; Android → Roboto; web apps → Inter, Roboto, system-ui. Also 2× Retina scale from DPI. On Apple platforms
the system font *is* SF, so the Swift masker already renders the right face for macOS/iOS screenshots.

## Scene text editing models (diffusion)

[TextCtrl](https://arxiv.org/pdf/2410.10133) (~1B, patch-level, best style preservation, English, blurs),
[TextFlux](https://github.com/yyyyyxie/textflux) / [FLUX-Text](https://github.com/AMAP-ML/FluxText) (FLUX-class
12B DiT, multilingual, GPU seconds), [AnyText2](https://arxiv.org/html/2411.15245v1),
[TextFlow training-free on FLUX-Kontext](https://arxiv.org/html/2603.24571),
[TextRIRO](https://github.com/YesianRohn/TextRIRO) (≤25 chars). None runs on a phone; all are seconds on a GPU
and introduce hallucinated glyphs. Useful as an optional "polish" backend on desktop/server, not the core path.

## Plan that fits the PoC

1. Heuristics first: stroke width → weight, shear → italic, page-relative. Pure Go + Swift.
2. Render-and-compare over a small per-OS candidate set; score per glyph; keep the best weight/size.
3. Sub-pixel: draw at the original baseline/x-height, match letter spacing to the crop's advance widths.
4. Optional: font-class model (storia ONNX) to pick the candidate set when the OS is unknown.
5. Optional: TextCtrl on desktop for the few lines where similarity stays below a threshold.
