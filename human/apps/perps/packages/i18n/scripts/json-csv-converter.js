const { validateI18nValue } = require("./utils");
const fs = require("fs");
const path = require("path");

const separator = ".";

function multiJson2Csv(jsonList, header) {
  if (!Array.isArray(jsonList) || jsonList.length === 0) {
    throw "Object array please.";
  }

  const ignoreKeys = loadIgnoreKeys();
  const baseJson = jsonList[0];
  const baseKeys = Object.keys(baseJson);
  const errors = {};
  let result = header.length ? [header] : [];
  for (const key of baseKeys) {
    const values = [];
    for (const [index, json] of jsonList.entries()) {
      const val = json[key] || "";
      values.push(val);
      // Skip validation if key is in ignore list
      if (!shouldIgnoreKey(key, ignoreKeys)) {
        const bool = validateI18nValue(val);
        if (!bool.valid) {
          const locale = header[index + 1];
          if (!errors[locale]) {
            errors[locale] = {};
          }
          errors[locale][key] = baseJson[key];
        }
      }
    }
    result.push(stringsToCsvLine([key, ...values]));
  }
  if (Object.keys(errors).length > 0) {
    throw new Error(
      "valid i18n value failed, please check the value of the following values: " +
        JSON.stringify(errors, null, 4),
    );
  }
  return result.join("\n");
}

function csv2multiJson(csv) {
  if (typeof csv !== "string") throw "String please.";
  const json = {};

  const ignoreKeys = loadIgnoreKeys();
  const lines = csvToLines(csv);
  const csvLines = lines.filter(Boolean).map(parseCsvLine);

  const headers = csvLines.shift();

  const errors = {};
  for (const [index, header] of headers.entries()) {
    json[header] = {};
    for (const line of csvLines) {
      const [key, ...values] = line;
      if (!key) {
        continue;
      }
      const val = values[index];
      json[header][key] = val;
      // Skip validation if key is in ignore list
      if (!shouldIgnoreKey(key, ignoreKeys)) {
        const bool = validateI18nValue(val);
        if (!bool.valid) {
          if (!errors[header]) {
            errors[header] = {};
          }
          errors[header][key] = values[0];
        }
      }
    }

    if (Object.values(json[header]).every((value) => !value)) {
      delete json[header];
    }
  }

  if (Object.keys(errors).length > 0) {
    throw new Error(
      "valid i18n value failed, please check the value of the following values: " +
        JSON.stringify(errors, null, 4),
    );
  }

  return json;
}

function json2Csv(json) {
  if (typeof json !== "object" || !json) throw "Object please.";
  return findKeyValues(json).join("\n");
}

function csv2Json(csv) {
  if (typeof csv !== "string") throw "String please.";
  const json = {};
  parseCsvLines(csv).forEach((line) => {
    const [path, value] = line;
    const pathSplit = path.split(separator);
    deepSet(json, pathSplit, value);
  });
  return json;
}

function findKeyValues(o, prefix, l = 0) {
  let result = [];
  for (const key of Object.keys(o)) {
    const fullKey = prefix ? prefix + separator + key : key;
    const value = o[key];
    if (typeof value === "string") {
      result.push(stringsToCsvLine([fullKey, value]));
    } else if (typeof value === "object" && value) {
      result = result.concat(findKeyValues(value, fullKey, l + 1));
    }
  }
  return result;
}

function stringsToCsvLine(line) {
  return line.map((s) => `"${s.replace(/"/g, '""')}"`).join(",");
}

function deepSet(o, path, value) {
  if (path.length > 1) {
    const key = path[0];
    if (!o[key]) {
      o[key] = {};
    }
    deepSet(o[key], path.slice(1), value);
  } else {
    o[path[0]] = value;
  }
}

function parseCsvLines(csv) {
  const lines = csvToLines(csv);
  return lines.filter(Boolean).map(parseCsvLine);
}

function csvToLines(csv) {
  return csv.split(/[\r\n]+/);
}

function parseCsvLine(line) {
  const result = [];
  for (let i = 0; i < line.length; i++) {
    const char = line[i];
    switch (char) {
      case ",":
        continue;
      case '"': {
        i++;
        const [str, i2] = parseCsvString(line, i);
        i = i2;
        result.push(str);
        break;
      }
      default: {
        const [str, i2] = parseUnquotedField(line, i);
        i = i2;
        result.push(str);
        break;
      }
    }
  }
  return result;
}

/** Parses one field until the next comma or end of line (for unquoted CSV). */
function parseUnquotedField(line, i) {
  let result = "";
  for (; i < line.length; i++) {
    if (line[i] === ",") break;
    result += line[i];
  }
  return [result, i];
}

function parseCsvString(line, i) {
  let result = "";
  loop: for (; i < line.length; i++) {
    const char = line[i];
    switch (char) {
      case '"':
        i++;
        if (line[i] === '"') {
          result += '"';
        } else {
          break loop;
        }
        break;
      default:
        result += char;
    }
  }
  return [result, i];
}

function getMissingKeys(jsonList, header) {
  if (!Array.isArray(jsonList) || jsonList.length === 0) {
    throw "Object array please.";
  }

  const baseJson = jsonList[0];
  const baseKeys = Object.keys(baseJson);
  const errors = {};
  for (const key of baseKeys) {
    for (const [index, json] of jsonList.entries()) {
      const val = json[key] || "";
      const bool = validateI18nValue(val);
      if (!bool.valid) {
        const locale = header[index + 1];
        if (!errors[locale]) {
          errors[locale] = {};
        }
        errors[locale][key] = baseJson[key];
      }
    }
  }
  return errors;
}

// Load ignore validation keys configuration
function loadIgnoreKeys() {
  const configPath = path.join(__dirname, "../validation-ignore.json");
  try {
    if (fs.existsSync(configPath)) {
      const config = JSON.parse(fs.readFileSync(configPath, "utf8"));
      const ignoreKeys = config.ignoreKeys || [];
      // Convert config to matcher array: string or regex
      return ignoreKeys
        .map((item) => {
          if (typeof item === "string") {
            // If string starts and ends with /, treat as regex pattern
            if (item.startsWith("/") && item.endsWith("/") && item.length > 2) {
              try {
                const pattern = item.slice(1, -1); // Remove leading and trailing /
                return new RegExp(pattern);
              } catch (error) {
                console.warn(
                  `Warning: Invalid regex pattern "${item}": ${error.message}`,
                );
                return item;
              }
            }
            return item;
          }
          return item;
        })
        .filter(Boolean);
    }
  } catch (error) {
    console.warn(
      `Warning: Failed to load validation-ignore.json: ${error.message}`,
    );
  }
  return [];
}

// Check if key should be ignored
function shouldIgnoreKey(key, ignoreKeys) {
  for (const matcher of ignoreKeys) {
    if (matcher instanceof RegExp) {
      if (matcher.test(key)) {
        return true;
      }
    } else if (typeof matcher === "string") {
      if (matcher === key) {
        return true;
      }
    }
  }
  return false;
}

module.exports = {
  separator,
  json2Csv,
  csv2Json,
  multiJson2Csv,
  csv2multiJson,
  getMissingKeys,
};
