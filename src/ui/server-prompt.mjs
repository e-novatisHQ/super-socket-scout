import { cursorHide } from "@inquirer/ansi";
import {
  createPrompt, isDownKey, isEnterKey, isUpKey, useKeypress, usePagination, useState,
} from "@inquirer/core";

function selectable(choice) { return !choice.disabled && choice.value; }

export function shortcutAction(key, scopes) {
  const name = key.name ?? key.sequence;
  if (name === "q") return { type: "quit" };
  if (name === "r") return { type: "refresh" };
  if (name === "e") return { type: "elevation" };
  if (name === "u") return { type: "udp" };
  if (name === "/") return { type: "search" };
  if (key.name === "left" || key.name === "tab" && key.shift) return { type: "scope", direction: -1 };
  if (key.name === "right" || key.name === "tab") return { type: "scope", direction: 1 };
  const index = Number(name) - 1;
  if (Number.isInteger(index) && scopes[index]) return { type: "scope-direct", scope: scopes[index] };
  return null;
}

export const serverPrompt = createPrompt((config, done) => {
  const items = config.choices.length ? config.choices : [{ name: "Aucun serveur dans cette vue", value: null, disabled: true }];
  const selectableIndexes = items.flatMap((item, index) => selectable(item) ? [index] : []);
  const [active, setActive] = useState(selectableIndexes[0] ?? 0);

  useKeypress((key, rl) => {
    const shortcut = shortcutAction(key, config.scopes);
    if (shortcut) { rl.clearLine(0); done(shortcut); return; }
    if (!selectableIndexes.length) return;
    if (isEnterKey(key)) { done(items[active].value); return; }
    if (isUpKey(key) || isDownKey(key)) {
      rl.clearLine(0);
      const position = selectableIndexes.indexOf(active);
      const offset = isUpKey(key) ? -1 : 1;
      setActive(selectableIndexes[(position + offset + selectableIndexes.length) % selectableIndexes.length]);
    }
  });

  const page = usePagination({
    items,
    active,
    pageSize: config.pageSize,
    loop: true,
    renderItem({ item, isActive }) {
      if (item.disabled) return `\x1b[2m  ${item.name}\x1b[0m`;
      return `${isActive ? "\x1b[36m› " : "  "}${item.name}${isActive ? "\x1b[0m" : ""}`;
    },
  });

  return `${page}\n\n\x1b[2m↑↓ serveur · Entrée détails · ←→ vue · 1–7 accès direct\n/ recherche · u UDP · e sudo · r rafraîchir · q quitter\x1b[0m${cursorHide}`;
});
