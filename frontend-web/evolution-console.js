(function exposeAgentEvolutionConsole(globalObject) {
  "use strict";

  const routeDefaults = Object.freeze({
    view: "inbox",
    risk: [],
    type: "",
    run: "",
    tab: "comparison",
    cursor: "",
    drawer: "",
  });
  const validViews = new Set(["inbox", "fleet", "history", "rules"]);
  const validRisks = new Set(["p0", "p1", "p2", "p3"]);
  const validTypes = new Set(["agent_policy", "knowledge_release", "combined"]);
  const validTabs = new Set(["comparison", "evidence", "audit"]);
  const riskOrder = Object.freeze({ p0: 0, p1: 1, p2: 2, p3: 3 });
  const riskAliases = Object.freeze({
    p0: "p0", critical: "p0",
    p1: "p1", high: "p1",
    p2: "p2", medium: "p2",
    p3: "p3", low: "p3",
  });
  const riskQueryAliases = Object.freeze({
    p0: ["p0", "critical"],
    p1: ["p1", "high"],
    p2: ["p2", "medium"],
    p3: ["p3", "low"],
  });

  function normalizeRisk(value) {
    return riskAliases[String(value || "").trim().toLowerCase()] || "";
  }

  function riskLabel(value) {
    return ({ p0: "P0 紧急", p1: "P1 高", p2: "P2 中", p3: "P3 低" })[normalizeRisk(value)] || "风险未知";
  }

  function expandRiskQuery(values) {
    const expanded = [];
    const seen = new Set();
    for (const value of Array.isArray(values) ? values : []) {
      const canonical = normalizeRisk(value);
      for (const alias of riskQueryAliases[canonical] || []) {
        if (seen.has(alias)) continue;
        seen.add(alias);
        expanded.push(alias);
      }
    }
    return expanded;
  }

  function parseRoute(input) {
    const url = new URL(String(input || "/agent-packages"), "https://kbase.invalid");
    const risk = String(url.searchParams.get("risk") || "")
      .split(",")
      .map((value) => value.trim().toLowerCase())
      .filter((value, index, values) => validRisks.has(value) && values.indexOf(value) === index)
      .sort((left, right) => riskOrder[left] - riskOrder[right]);
    const view = String(url.searchParams.get("view") || "");
    const type = String(url.searchParams.get("type") || "");
    const tab = String(url.searchParams.get("tab") || "");
    return {
      view: validViews.has(view) ? view : routeDefaults.view,
      risk,
      type: validTypes.has(type) ? type : "",
      run: String(url.searchParams.get("run") || ""),
      tab: validTabs.has(tab) ? tab : routeDefaults.tab,
      cursor: String(url.searchParams.get("cursor") || ""),
      drawer: url.searchParams.get("drawer") === "compiler" ? "compiler" : "",
    };
  }

  function serializeRoute(state = {}) {
    const params = new URLSearchParams();
    const view = validViews.has(state.view) ? state.view : routeDefaults.view;
    params.set("view", view);
    const risks = Array.isArray(state.risk)
      ? state.risk.filter((value, index, values) => validRisks.has(value) && values.indexOf(value) === index)
        .sort((left, right) => riskOrder[left] - riskOrder[right])
      : [];
    if (risks.length) params.set("risk", risks.join(","));
    if (validTypes.has(state.type)) params.set("type", state.type);
    if (state.run) params.set("run", String(state.run));
    if (validTabs.has(state.tab) && state.tab !== routeDefaults.tab) params.set("tab", state.tab);
    if (state.cursor) params.set("cursor", String(state.cursor));
    if (state.drawer === "compiler") params.set("drawer", "compiler");
    return `/agent-packages?${params.toString()}`;
  }

  function packageSort(left, right) {
    const publishedDelta = Date.parse(right?.published_at || "") - Date.parse(left?.published_at || "");
    if (Number.isFinite(publishedDelta) && publishedDelta !== 0) return publishedDelta;
    return String(right?.version || "").localeCompare(String(left?.version || ""), undefined, { numeric: true });
  }

  function selectCurrentPublished(packages) {
    const stable = Array.isArray(packages) ? [...packages].sort(packageSort) : [];
    return stable.find((pkg) => pkg?.lifecycle_state === "published") || stable[0] || null;
  }

  function groupPackages(packages) {
    const groups = new Map();
    for (const pkg of Array.isArray(packages) ? packages : []) {
      const packageID = String(pkg?.package_id || "").trim();
      if (!packageID) continue;
      if (!groups.has(packageID)) groups.set(packageID, []);
      groups.get(packageID).push(pkg);
    }
    return Array.from(groups, ([packageID, versions]) => {
      const history = [...versions].sort(packageSort);
      return { package_id: packageID, current: selectCurrentPublished(history), history };
    }).sort((left, right) => left.package_id.localeCompare(right.package_id));
  }

  function sortRuns(runs) {
    return Array.isArray(runs) ? [...runs].sort((left, right) => {
      const riskDelta = (riskOrder[normalizeRisk(left?.risk_level)] ?? 99) - (riskOrder[normalizeRisk(right?.risk_level)] ?? 99);
      if (riskDelta) return riskDelta;
      const priorityDelta = Number(right?.priority_score || 0) - Number(left?.priority_score || 0);
      if (priorityDelta) return priorityDelta;
      const timeDelta = Date.parse(right?.updated_at || "") - Date.parse(left?.updated_at || "");
      if (Number.isFinite(timeDelta) && timeDelta !== 0) return timeDelta;
      return String(left?.run_id || "").localeCompare(String(right?.run_id || ""));
    }) : [];
  }

  function scoreDelta(baseline, candidate) {
    if (baseline === null || baseline === undefined || candidate === null || candidate === undefined) return null;
    const left = Number(baseline);
    const right = Number(candidate);
    if (!Number.isFinite(left) || !Number.isFinite(right)) return null;
    return Math.round((right - left) * 10) / 10;
  }

  function shouldHandleClick(event = {}, anchor = {}) {
    return (event.button === undefined || event.button === 0) &&
      !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey &&
      !anchor.hasAttribute?.("download") &&
      (!anchor.target || anchor.target === "_self");
  }

  function activateDialog(dialog, fallbackBackground = []) {
    if (!dialog) return "unavailable";
    let mode = "native";
    if (typeof dialog.showModal === "function") {
      if (!dialog.open) dialog.showModal();
    } else {
      mode = "fallback";
      dialog.setAttribute?.("open", "");
      dialog.setAttribute?.("role", "dialog");
      dialog.setAttribute?.("aria-modal", "true");
      for (const element of Array.isArray(fallbackBackground) ? fallbackBackground : []) {
        if (element) element.inert = true;
      }
    }
    dialog.setAttribute?.("data-evolution-modal-mode", mode);
    dialog.querySelector?.("[data-evolution-drawer-close], button:not([disabled]), select:not([disabled]), input:not([disabled])")?.focus?.();
    return mode;
  }

  function bindDialogDismiss(dialog, onDismiss) {
    if (!dialog?.addEventListener || typeof onDismiss !== "function") return () => {};
    let dismissed = false;
    const dismiss = (event) => {
      event?.preventDefault?.();
      if (dismissed) return;
      dismissed = true;
      onDismiss();
    };
    const cancelHandler = (event) => dismiss(event);
    const keydownHandler = (event) => {
      if (event?.key === "Escape") dismiss(event);
    };
    dialog.addEventListener("cancel", cancelHandler);
    dialog.addEventListener("keydown", keydownHandler);
    return () => {
      dialog.removeEventListener?.("cancel", cancelHandler);
      dialog.removeEventListener?.("keydown", keydownHandler);
    };
  }

  globalObject.AgentEvolutionConsole = Object.freeze({
    routeDefaults,
    normalizeRisk,
    riskLabel,
    expandRiskQuery,
    parseRoute,
    serializeRoute,
    groupPackages,
    selectCurrentPublished,
    sortRuns,
    scoreDelta,
    shouldHandleClick,
    activateDialog,
    bindDialogDismiss,
  });
}(globalThis));
