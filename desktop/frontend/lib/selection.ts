/** 可整块全选的内容: 代码块与显式标记的区域。 */
const SELECT_ALL_BLOCK = "pre, [data-select-all]";

const NON_TEXT_INPUTS = new Set(["button", "checkbox", "color", "file", "image", "radio", "range", "reset", "submit"]);

function isEditable(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable || target instanceof HTMLTextAreaElement) return true;
  return target instanceof HTMLInputElement && !NON_TEXT_INPUTS.has(target.type);
}

/**
 * 客户端里接管 Ctrl/⌘+A: 焦点在输入框或可编辑区 (含 xterm 的输入框) 时照常全选;
 * 否则不再全选整页, 最近一次点击落在代码块等整块内容里时只选这一块, 其余什么都不选。
 * noVNC 画面自己拦下按键转发给远端, 事件到不了 document。
 */
export function installSelectAllGuard(): () => void {
  if (!document.documentElement.hasAttribute("data-app-shell")) return () => {};
  const mac = document.documentElement.classList.contains("mac");
  let block: Element | null = null;

  const onPointerDown = (e: PointerEvent) => {
    block = e.target instanceof Element ? e.target.closest(SELECT_ALL_BLOCK) : null;
  };
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.defaultPrevented || e.altKey || e.shiftKey || e.key.toLowerCase() !== "a") return;
    if (!(mac ? e.metaKey : e.ctrlKey) || isEditable(e.target)) return;
    e.preventDefault();
    const selection = window.getSelection();
    if (!selection) return;
    if (block?.isConnected) selection.selectAllChildren(block);
    else selection.removeAllRanges();
  };

  document.addEventListener("pointerdown", onPointerDown, true);
  document.addEventListener("keydown", onKeyDown);
  return () => {
    document.removeEventListener("pointerdown", onPointerDown, true);
    document.removeEventListener("keydown", onKeyDown);
  };
}
