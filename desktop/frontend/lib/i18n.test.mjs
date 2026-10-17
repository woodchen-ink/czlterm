import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import vm from "node:vm";
import test from "node:test";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const ts = require("typescript");
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

/** 在隔离的浏览器桩里加载真实 TypeScript 模块，不写编译产物、不接触业务数据。 */
function harness({ language = "en-US", stored = null, storageBlocked = false } = {}) {
  const cache = new Map();
  let saved = stored;
  const browser = {
    navigator: { language },
    document: { documentElement: { lang: "zh-CN" } },
    localStorage: {
      getItem: () => { if (storageBlocked) throw new Error("blocked"); return saved; },
      setItem: (_key, value) => { if (storageBlocked) throw new Error("blocked"); saved = value; },
    },
  };
  const context = vm.createContext({ ...browser, console, setTimeout, clearTimeout, queueMicrotask });
  // 使用与组件相同的 React 实例，SSR 验证只隔离 Wails 与保险库状态。
  const vault = { useVault: () => ({ status: null, refreshStatus: async () => {} }) };
  function load(filename) {
    if (filename === "@/components/vault-provider") return vault;
    if (!filename.startsWith("@/")) return require(filename);
    const base = path.join(root, filename.slice(2));
    const file = [base, `${base}.ts`, `${base}.tsx`, `${base}.js`].find((candidate) => fs.existsSync(candidate) && fs.statSync(candidate).isFile());
    if (!file) throw new Error(`Module not found: ${filename}`);
    if (cache.has(file)) return cache.get(file).exports;
    const loadedModule = { exports: {} };
    cache.set(file, loadedModule);
    const output = ts.transpileModule(fs.readFileSync(file, "utf8"), {
      fileName: file,
      compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2020, esModuleInterop: true },
    }).outputText;
    const fn = vm.runInContext(`(function(require,module,exports){${output}\n})`, context);
    fn(load, loadedModule, loadedModule.exports);
    return loadedModule.exports;
  }
  return { load, browser, saved: () => saved };
}

test("system locale, manual override, persistence and document language", () => {
  const h = harness();
  const i18n = h.load("@/lib/i18n");
  i18n.initializeLanguage();
  assert.equal(i18n.getLocale(), "en");
  assert.equal(i18n.t("设置"), "Settings");
  i18n.setLanguage("zh-CN");
  assert.equal(i18n.t("设置"), "设置");
  assert.equal(h.saved(), "zh-CN");
  assert.equal(h.browser.document.documentElement.lang, "zh-CN");
  assert.equal(i18n.resolveLocale("system", "zh-Hant-TW"), "zh-CN");
  assert.equal(i18n.resolveLocale("system", "de-DE"), "en");
  assert.equal(i18n.resolveLocale("en", "zh-CN"), "en");
});

test("stored preference and corrupt or inaccessible storage", () => {
  for (const stored of ["en", "zh-CN", "system", "bad", null]) {
    const h = harness({ stored });
    const i18n = h.load("@/lib/i18n");
    i18n.initializeLanguage();
    assert.equal(i18n.getLocale(), stored === "zh-CN" ? "zh-CN" : "en");
  }
  const h = harness({ storageBlocked: true });
  const i18n = h.load("@/lib/i18n");
  assert.doesNotThrow(() => i18n.initializeLanguage());
  assert.doesNotThrow(() => i18n.setLanguage("zh-CN"));
  assert.equal(i18n.getLocale(), "zh-CN");
});

test("subscriptions include system-language changes and can unsubscribe", () => {
  const h = harness();
  const i18n = h.load("@/lib/i18n");
  let changes = 0;
  const off = i18n.subscribeLanguage(() => changes++);
  i18n.initializeLanguage();
  h.browser.navigator.language = "zh-CN";
  i18n.setLanguage("system", false);
  assert.equal(i18n.getLocale(), "zh-CN");
  assert.equal(changes, 2);
  off();
  i18n.setLanguage("en");
  assert.equal(changes, 2);
});

test("sentence interpolation preserves user text and placeholder-like data", () => {
  const h = harness();
  const { initializeLanguage, t } = h.load("@/lib/i18n");
  initializeLanguage();
  assert.equal(t("正在 {0} 上运行 {1}", "生产{1}", "备份脚本"), "Running 备份脚本 on 生产{1}");
  assert.equal(t("未登记的文案"), "未登记的文案");
  assert.equal(t("删除 {0}?", "服务器"), "Delete 服务器?");
});

test("errors retain codes and duration formatting follows the active language", () => {
  const h = harness();
  const i18n = h.load("@/lib/i18n");
  const { errorText } = h.load("@/lib/errors");
  const { formatDuration } = h.load("@/lib/format");
  i18n.initializeLanguage();
  assert.equal(errorText("3001 vault locked"), "The vault is locked. Unlock it first (3001)");
  assert.equal(errorText("9999 new error"), "new error (9999)");
  assert.equal(errorText(null), "Unknown error");
  assert.equal(formatDuration(90060), "1d 1h");
  i18n.setLanguage("zh-CN");
  assert.equal(errorText("3001 vault locked"), "保险库已锁定, 请先解锁 (3001)");
  assert.equal(formatDuration(90060), "1 天 1 小时");
});

test("all UI translation keys and error codes have English entries with matching parameters", () => {
  const h = harness();
  const { english } = h.load("@/lib/i18n-messages");
  const { englishErrors } = h.load("@/lib/i18n-errors");
  const files = fs.readdirSync(path.join(root, "components"))
    .filter((name) => name.endsWith(".tsx"))
    .map((name) => path.join(root, "components", name));
  files.push(path.join(root, "lib/format.ts"), path.join(root, "components/ui/dialog.tsx"));
  for (const file of files) {
    const source = ts.createSourceFile(file, fs.readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true);
    function visit(node) {
      if (ts.isCallExpression(node) && node.expression.getText(source) === "t" && node.arguments[0] && ts.isStringLiteral(node.arguments[0])) {
        assert.ok(english[node.arguments[0].text], `Missing English: ${node.arguments[0].text}`);
      }
      ts.forEachChild(node, visit);
    }
    visit(source);
  }
  for (const [key, value] of Object.entries(english)) {
    const params = (text) => [...new Set(text.match(/\{\d+\}/g) ?? [])].sort();
    assert.deepEqual(params(value), params(key), `Parameter mismatch: ${key}`);
  }
  const codes = [...fs.readFileSync(path.join(root, "lib/errors.ts"), "utf8").matchAll(/"(\d{4})":/g)].map((match) => match[1]);
  assert.deepEqual(Object.keys(englishErrors).sort(), codes.sort());
});

test("general settings render in English and Chinese without requiring backend settings", () => {
  const h = harness();
  const React = require("react");
  const { renderToStaticMarkup } = require("react-dom/server");
  const i18n = h.load("@/lib/i18n");
  const { SettingsPage } = h.load("@/components/settings-page");
  i18n.initializeLanguage();
  const props = { onClose() {}, onSaved() {} };
  const en = renderToStaticMarkup(React.createElement(SettingsPage, props));
  assert.ok(en.includes("General"));
  assert.ok(en.includes("Settings"));
  assert.ok(en.includes("Language / 语言"));
  i18n.setLanguage("zh-CN");
  const zh = renderToStaticMarkup(React.createElement(SettingsPage, props));
  assert.ok(zh.includes("通用"));
  assert.ok(zh.includes("设置"));
});
