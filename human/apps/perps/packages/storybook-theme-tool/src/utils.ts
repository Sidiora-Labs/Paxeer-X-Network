export function isColorValue(value: string) {
  return (
    value.startsWith("--oui-color") ||
    (value.startsWith("--oui-gradient") &&
      !["stop-start", "stop-end", "angle"].some((item) => value.endsWith(item)))
  );
}

export function hexToRgb(hex: string) {
  if (!/^#([A-Fa-f0-9]{3}){1,2}$/.test(hex)) {
    console.error("Invalid HEX color format");
    return null;
  }

  let color = hex.substring(1);

  if (color.length === 3) {
    color = color
      .split("")
      .map((char) => char + char)
      .join("");
  }

  const r = parseInt(color.substring(0, 2), 16);
  const g = parseInt(color.substring(2, 4), 16);
  const b = parseInt(color.substring(4, 6), 16);

  return `${r} ${g} ${b}`;
}

export function rgbToHex(rgb: string[]) {
  return `#${rgb
    .map((c) => parseInt(c).toString(16).padStart(2, "0"))
    .join("")}`;
}

export function object2Css(theme: Record<string, string>) {
  const newTheme: Record<string, string> = {};

  for (const [key, value] of Object.entries(theme)) {
    // console.log(key, value);
    if (value === "") continue;

    let val = value;

    if (isColorValue(key)) {
      val = hexToRgb(value) ?? "";
    }

    newTheme[key] = val;
  }

  const code = JSON.stringify(newTheme, null, 2)
    .replace(/"/g, "")
    .replace(/,\n/g, ";\n")
    .replace(/(\s*})$/, ";\n}")
    .replace(/\\/g, '"');

  return `:root ${code}`;
}

export function parseCssToJson(cssText: string) {
  const css = cssText.replace(/:root\s*{([\s\S]*?)}/, "$1");
  const lines = css
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line);
  const json: Record<string, string> = {};

  lines.forEach((line) => {
    const [key, value] = line.split(":").map((item) => item.trim());
    let formattedValue = value.replace(";", "");

    if (isColorValue(key)) {
      const rgb = value.split(/\s+/);
      formattedValue = rgbToHex(rgb);
    }
    json[key] = formattedValue;
  });
  return json;
}
