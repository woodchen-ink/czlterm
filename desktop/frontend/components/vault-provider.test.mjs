import assert from "node:assert/strict";
import fs from "node:fs";
import { createRequire } from "node:module";
import vm from "node:vm";
import test from "node:test";

const require = createRequire(import.meta.url);
const ts = require("typescript");
const source = fs.readFileSync(new URL("./vault-provider.tsx", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2020 },
}).outputText;

/** 使用可控 Promise、时钟和浏览器事件执行真实 Provider 的状态同步逻辑。 */
function harness() {
  const states = [], effects = [], requests = [], order = [];
  const events = new Map(), windowEvents = new Map(), documentEvents = new Map(), timers = new Map();
  const react = {
    createContext: () => ({ Provider: "provider" }),
    useCallback: (fn) => fn,
    useContext: () => null,
    useEffect: (fn) => effects.push(fn),
    useRef: (value) => ({ current: value }),
    useState: (value) => {
      const index = states.length;
      states.push(value);
      return [value, (next) => { states[index] = next; }];
    },
  };
  const api = {
    VaultStatus: () => {
      order.push("query");
      return new Promise((resolve, reject) => requests.push({ resolve, reject }));
    },
    VaultLock: async () => {},
  };
  const browserWindow = {
    addEventListener: (event, fn) => windowEvents.set(event, fn),
    removeEventListener: (event) => windowEvents.delete(event),
    setInterval: (fn) => { timers.set(1, fn); return 1; },
    clearInterval: (id) => timers.delete(id),
  };
  const document = {
    visibilityState: "visible",
    addEventListener: (event, fn) => documentEvents.set(event, fn),
    removeEventListener: (event) => documentEvents.delete(event),
  };
  /** 除 React hooks 与平台接口外, 不替换被测实现。 */
  function load(name) {
    if (name === "react") return react;
    if (name === "@/lib/i18n") return { t: (value) => value };
    if (name === "@/components/language-provider") return { useLanguage: () => {} };
    if (name === "@/components/unlock-dialog") return { UnlockDialog: "unlock-dialog" };
    if (name === "@/lib/errors") return { isLockedError: () => true };
    if (name === "sonner") return { toast: { info: () => {} } };
    if (name === "@/lib/api") return {
      api, Events: { vaultLocked: "locked", vaultUnlocked: "unlocked" },
      onEvent: (event, fn) => {
        order.push(event);
        events.set(event, fn);
        return () => events.delete(event);
      },
    };
    return require(name);
  }
  const context = vm.createContext({ window: browserWindow, document, queueMicrotask: (fn) => fn() });
  const loadedModule = { exports: {} };
  vm.runInContext(`(function(require,module,exports){${compiled}\n})`, context)(load, loadedModule, loadedModule.exports);
  const tree = loadedModule.exports.VaultProvider({ children: null });
  const cleanup = effects[0]();
  return { states, requests, order, events, windowEvents, documentEvents, document, timers,
    value: tree.props.value, dialog: tree.props.children[1].props, cleanup, tick: () => timers.get(1)?.() };
}

/** 等待查询回调与等待解锁的 Promise 全部结算。 */
const flush = () => new Promise((resolve) => setImmediate(resolve));

test("subscribe before initial status and ignore late startup response", async () => {
  const h = harness();
  assert.deepEqual(h.order, ["locked", "unlocked", "query"]);
  h.events.get("unlocked")();
  h.requests[1].resolve({ status: "unlocked" });
  await flush();
  h.requests[0].resolve({ status: "locked" });
  await flush();
  assert.equal(h.states[0].status, "unlocked");
  h.cleanup();
});

test("poll missed restore events, skip pending/hidden/unlocked checks", async () => {
  const h = harness();
  h.tick();
  assert.equal(h.requests.length, 1);
  h.requests[0].resolve({ status: "locked" });
  await flush();
  h.document.visibilityState = "hidden";
  h.tick();
  assert.equal(h.requests.length, 1);
  h.document.visibilityState = "visible";
  h.tick();
  h.requests[1].resolve({ status: "unlocked" });
  await flush();
  assert.equal(h.states[0].status, "unlocked");
  h.tick();
  assert.equal(h.requests.length, 2);
  h.cleanup();
});

test("failed status checks retry on the next visible tick", async () => {
  const h = harness();
  h.requests[0].reject(new Error("temporarily unavailable"));
  await flush();
  h.tick();
  assert.equal(h.requests.length, 2);
  h.cleanup();
  h.requests[1].resolve({ status: "unlocked" });
  await flush();
  assert.equal(h.states[0], null);
});

test("focus and visibility refresh state; cleanup removes all listeners", () => {
  const h = harness();
  h.windowEvents.get("focus")();
  h.documentEvents.get("visibilitychange")();
  assert.equal(h.requests.length, 3);
  h.cleanup();
  for (const listeners of [h.events, h.windowEvents, h.documentEvents, h.timers]) {
    assert.equal(listeners.size, 0);
  }
});

test("do not open a password dialog when restoration already finished", async () => {
  const h = harness();
  const unlocked = h.value.requestUnlock();
  h.requests[1].resolve({ status: "unlocked" });
  assert.equal(await unlocked, true);
  assert.equal(h.states[1], false);
  h.cleanup();
});

test("background restore closes an open dialog and resumes waiting operations", async () => {
  const h = harness();
  const unlocked = h.value.requestUnlock();
  h.requests[1].resolve({ status: "locked" });
  await flush();
  assert.equal(h.states[1], true);
  h.events.get("unlocked")();
  h.requests[2].resolve({ status: "unlocked" });
  assert.equal(await unlocked, true);
  assert.equal(h.states[1], false);
  h.cleanup();
});

test("late unlocked response cannot undo a newer lock notification", async () => {
  const h = harness();
  h.events.get("locked")();
  h.requests[1].resolve({ status: "locked" });
  await flush();
  h.requests[0].resolve({ status: "unlocked" });
  await flush();
  assert.equal(h.states[0].status, "locked");
  h.cleanup();
});

test("stale dialog success cannot resume waiters after a newer lock", async () => {
  const h = harness();
  const unlocked = h.value.requestUnlock();
  h.requests[1].resolve({ status: "locked" });
  await flush();
  let resumed = false;
  void unlocked.then(() => { resumed = true; });
  const callback = h.dialog.onUnlocked();
  h.events.get("locked")();
  h.requests[3].resolve({ status: "locked" });
  await flush();
  h.requests[2].resolve({ status: "unlocked" });
  await callback;
  await flush();
  assert.equal(h.states[0].status, "locked");
  assert.equal(h.states[1], true);
  assert.equal(resumed, false);
  h.dialog.onOpenChange(false);
  assert.equal(await unlocked, false);
  h.cleanup();
});

test("unmount cancels queued unlock waiters", async () => {
  const h = harness();
  const unlocked = h.value.requestUnlock();
  h.requests[1].resolve({ status: "locked" });
  await flush();
  h.cleanup();
  assert.equal(await unlocked, false);
});

test("an unlock request awaiting status cannot reopen after unmount", async () => {
  const h = harness();
  const unlocked = h.value.requestUnlock();
  h.cleanup();
  h.requests[1].resolve({ status: "locked" });
  assert.equal(await unlocked, false);
  assert.equal(h.states[1], false);
});
