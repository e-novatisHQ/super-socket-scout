export const EXIT = Object.freeze({ SUCCESS: 0, ERROR: 1, CONFLICT: 2, INVALID: 3, CONFIRMATION: 4, CANCELLED: 130 });

export function validateItems(items) {
  if (!Array.isArray(items)) throw new Error("L'inventaire doit être un tableau");
  const ids = new Set();
  for (const item of items) {
    if (!item?.id || ids.has(item.id)) throw new Error(`Identifiant invalide ou dupliqué : ${item?.id}`);
    ids.add(item.id);
  }
  return items;
}

export async function inventory(adapter) {
  return validateItems(await adapter.discover());
}

export async function stopServers({ adapter, ids, yes, confirmToken, includeSystem = false, confirm }) {
  const items = await inventory(adapter);
  const known = new Set(items.map((item) => item.id));
  const unknown = [...new Set(ids)].filter((id) => !known.has(id));
  if (unknown.length) return { code: EXIT.INVALID, plan: null, results: [], error: `Identifiant inconnu : ${unknown.join(", ")}` };
  const plan = await adapter.buildPlan(ids, { includeSystem });
  if (!plan.actions.length) return { code: plan.conflicts.length ? EXIT.CONFLICT : EXIT.SUCCESS, plan, results: plan.conflicts };
  const accepted = confirm
    ? await confirm(plan)
    : yes && confirmToken === plan.confirmationToken;
  if (!accepted) return { code: EXIT.CONFIRMATION, plan, results: [], error: "Confirmation requise ou incorrecte" };
  const results = await adapter.execute(plan);
  const code = results.some((r) => r.status === "failed") ? EXIT.ERROR
    : results.some((r) => r.status === "conflict") ? EXIT.CONFLICT : EXIT.SUCCESS;
  return { code, plan, results };
}
