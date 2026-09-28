import fs from 'node:fs';

const path = process.argv[2];
const dashboardPath = process.argv[3];
if (!path || !dashboardPath) {
  throw new Error('Selkies client and dashboard paths are required.');
}

const source = fs.readFileSync(path, 'utf8');
const needle = "const authToken = urlParams.get('token');";
if (!source.includes(needle)) {
  throw new Error('The pinned Selkies token bootstrap no longer matches.');
}

const replacement = `const fragmentParams = new URLSearchParams(window.location.hash.slice(1));
const fragmentToken = fragmentParams.get('token');
const authToken = urlParams.get('token') || fragmentToken;
if (fragmentToken) {
    history.replaceState(null, '', window.location.pathname);
}`;

fs.writeFileSync(path, source.replace(needle, replacement));

const dashboard = fs.readFileSync(dashboardPath, 'utf8');
const dashboardNeedle = '    <script type="module" src="src/main.jsx"></script>';
if (!dashboard.includes(dashboardNeedle)) {
  throw new Error('The pinned Selkies dashboard bootstrap no longer matches.');
}

const storageShim = `    <script>
      (() => {
        try {
          Object.defineProperty(window, 'devicePixelRatio', {
            configurable: true,
            get: () => 2,
          });
        } catch {}
        const memoryStorage = () => {
          const values = new Map();
          return {
            get length() { return values.size; },
            clear() { values.clear(); },
            getItem(key) {
              const normalized = String(key);
              return values.has(normalized) ? values.get(normalized) : null;
            },
            key(index) { return [...values.keys()][index] ?? null; },
            removeItem(key) { values.delete(String(key)); },
            setItem(key, value) { values.set(String(key), String(value)); },
          };
        };
        for (const name of ['localStorage', 'sessionStorage']) {
          try {
            void window[name].length;
          } catch {
            Object.defineProperty(window, name, {
              configurable: true,
              value: memoryStorage(),
            });
          }
        }
      })();
    </script>
    <style>
      html,
      body,
      #app {
        height: 100% !important;
        margin: 0 !important;
        overflow: hidden !important;
        width: 100% !important;
      }
      #root,
      #dashboard-root,
      #touch-gamepad-host {
        display: none !important;
      }
      #videoCanvas,
      #videoStream {
        height: 100% !important;
        inset: 0 !important;
        object-fit: fill !important;
        width: 100% !important;
      }
    </style>
${dashboardNeedle}`;

fs.writeFileSync(
  dashboardPath,
  dashboard.replace(dashboardNeedle, storageShim),
);
