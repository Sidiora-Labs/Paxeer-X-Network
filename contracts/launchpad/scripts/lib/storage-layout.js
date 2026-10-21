"use strict";
// Type identifiers include unstable AST ids; the recursively expanded type graph is authoritative.
function compareLayouts(baseline, current) {
  const errors = [];
  function shape(layout, id, seen = new Set()) {
    const t = layout.types && layout.types[id];
    if (!t) throw new Error(`missing type metadata ${id}`);
    if (seen.has(id)) return { recursive: t.label };
    const next = new Set(seen); next.add(id);
    const out = { encoding: t.encoding, bytes: t.numberOfBytes, label: t.label };
    for (const key of ["key", "value", "base"]) if (t[key]) out[key] = shape(layout, t[key], next);
    if (t.members) out.members = t.members.map(m => ({ label: m.label, slot: m.slot, offset: m.offset, type: shape(layout, m.type, next) }));
    return out;
  }
  try {
    if (!Array.isArray(baseline.storage) || !Array.isArray(current.storage)) throw new Error("missing storage table");
    for (let i = 0; i < baseline.storage.length; i++) {
      const b = baseline.storage[i], c = current.storage[i];
      if (!c) { errors.push(`removed ${b.label}`); continue; }
      if (b.label !== c.label || b.slot !== c.slot || b.offset !== c.offset) errors.push(`moved or renamed ${b.label}`);
      if (JSON.stringify(shape(baseline, b.type)) !== JSON.stringify(shape(current, c.type))) errors.push(`type graph changed ${b.label}`);
    }
    // Validate appended metadata too. Gaps are deliberately immutable: consumption requires an explicit migration qualification.
    for (const c of current.storage) shape(current, c.type);
    if (baseline.storage.length && current.storage.length > baseline.storage.length) {
      const b = baseline.storage[baseline.storage.length - 1];
      const end = BigInt(b.slot) * 32n + BigInt(b.offset) + BigInt(baseline.types[b.type].numberOfBytes);
      for (const c of current.storage.slice(baseline.storage.length)) if (BigInt(c.slot) * 32n + BigInt(c.offset) < end) errors.push(`appended slot overlaps baseline: ${c.label}`);
    }
  } catch (e) { errors.push(e.message); }
  return errors;
}
module.exports = { compareLayouts };
function readRequiredBaseline(file) {
  if (!require("fs").existsSync(file)) throw new Error(`Required storage baseline missing: ${file}`);
  const layout = JSON.parse(require("fs").readFileSync(file, "utf8"));
  const errors = compareLayouts(layout, layout);
  if (errors.length) throw new Error(`Invalid storage baseline: ${errors.join("; ")}`);
  return layout;
}
module.exports.readRequiredBaseline = readRequiredBaseline;
