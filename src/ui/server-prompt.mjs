import { cursorHide } from "@inquirer/ansi";
import {
  createPrompt, isDownKey, isEnterKey, isUpKey, useKeypress, usePagination, useState,
} from "@inquirer/core";

function selectable(choice) { return !choice.disabled && choice.value; }

function spread(parts, width) {
  const spaces = width - parts.reduce((total, part) => total + part.length, 0);
  if (parts.length === 1 || spaces < parts.length - 1) return parts.join("\n");
  const gaps = parts.length - 1;
  const gap = Math.min(6, Math.floor(spaces / gaps));
  return parts.map((part, index) => index === parts.length - 1
    ? part
    : `${part}${" ".repeat(gap)}`).join("");
}

export function helpLines(width = 100) {
  const navigation = "↑↓ serveur · Entrée détails";
  const views = "←→ vue · 1–4 accès direct";
  const filters = "/ recherche · u UDP · e sudo";
  const session = "r rafraîchir · q quitter";
  if (width >= 124) return spread([navigation, views, `${filters} · ${session}`], width);
  if (width >= 64) return `${spread([navigation, views], width)}\n${spread([filters, session], width)}`;
  return [navigation, views, filters, session].join("\n");
}

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
  const items = config.choices.length ? config.choices : [{ name: config.emptyLabel ?? "Aucun serveur dans cette vue", value: null, disabled: true }];
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
      return `${isActive ? "\x1b[36m›\x1b[0m " : "  "}${item.name}`;
    },
  });

  const description = selectableIndexes.length ? items[active]?.description : null;
  const separator = "─".repeat(config.width ?? 100);
  return `${page}\n\n\x1b[2m${separator}\x1b[0m${description ? `\n${description}` : ""}\n\n\x1b[2m${helpLines(config.width)}\x1b[0m${cursorHide}`;
});
