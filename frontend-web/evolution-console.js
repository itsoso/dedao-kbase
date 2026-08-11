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
  const inboxStatuses = Object.freeze([
    "detected", "triaged", "generating", "evaluating", "awaiting_approval",
    "approved", "publishing", "observing", "blocked", "failed",
  ]);
  const historyStatuses = Object.freeze(["completed", "rejected", "superseded", "rolled_back"]);

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

  function runStatusesForView(view) {
    if (view === "inbox") return [...inboxStatuses];
    if (view === "history") return [...historyStatuses];
    return [];
  }

  function buildRunsQuery(route = {}) {
    const statuses = runStatusesForView(route.view);
    if (!statuses.length) return null;
    const query = new URLSearchParams({ limit: "50", status: statuses.join(",") });
    const risks = expandRiskQuery(route.risk);
    if (risks.length) query.set("risk", risks.join(","));
    if (validTypes.has(route.type)) query.set("type", route.type);
    if (route.cursor) query.set("cursor", String(route.cursor));
    return query;
  }

  function detailTabForView(view) {
    return view === "history" ? "audit" : "comparison";
  }

  function detailPresentation(view) {
    if (view === "history") {
      return Object.freeze({
        backLabel: "返回演化历史",
        sectionLabel: "演化历史详情",
        tabsLabel: "审计详情",
      });
    }
    return Object.freeze({
      backLabel: "返回待办队列",
      sectionLabel: "线上版本对比",
      tabsLabel: "任务详情",
    });
  }

  function routePatchForView(view) {
    const nextView = validViews.has(view) ? view : routeDefaults.view;
    return { view: nextView, cursor: "", run: "", tab: detailTabForView(nextView) };
  }

  function navigationPatchForDataset(dataset = {}, state = {}) {
    const patch = {};
    if (dataset.evolutionRunId) {
      patch.run = String(dataset.evolutionRunId);
      patch.tab = detailTabForView(state?.route?.view);
    }
    if (dataset.evolutionView) {
      Object.assign(patch, routePatchForView(String(dataset.evolutionView)));
    }
    if (validTabs.has(dataset.evolutionDetailTab)) {
      patch.tab = dataset.evolutionDetailTab;
    }
    if (dataset.evolutionCursor) {
      patch.cursor = String(dataset.evolutionCursor);
      patch.run = "";
    }
    return patch;
  }

  function runsScopeKey(route = {}) {
    const risks = Array.isArray(route.risk)
      ? route.risk.map(normalizeRisk).filter(Boolean).sort((left, right) => riskOrder[left] - riskOrder[right])
      : [];
    return [route.view || routeDefaults.view, risks.join(","), route.type || "", route.cursor || ""].join("|");
  }

  function queuePresentation(view) {
    if (view === "history") {
      return Object.freeze({
        indexLabel: "01 / 按时间排序",
        title: "演化历史",
        sortHint: "按创建时间倒序",
        loading: "正在读取演化历史…",
        errorTitle: "演化历史不可用",
        emptyTitle: "暂无演化历史",
        emptyBody: "完成、拒绝、替代或回滚的任务会显示在这里。",
        detailPrompt: "选择一条历史记录",
      });
    }
    return Object.freeze({
      indexLabel: "01 / 按风险排序",
      title: "演化待办队列",
      sortHint: "按风险与优先级排序",
      loading: "正在读取优先队列…",
      errorTitle: "待办队列不可用",
      emptyTitle: "当前没有演化待办",
      emptyBody: "控制面运行正常，新信号会自动进入这里。",
      detailPrompt: "选择一项待办",
    });
  }

  function overviewStatusMetrics(overview = {}) {
    const openRuns = Array.isArray(overview.open_runs) ? overview.open_runs : [];
    return Object.freeze({
      awaitingApproval: Number(overview.awaiting_approval || 0),
      blocked: Number(overview.blocked || 0),
      staleKnowledge: openRuns.filter((run) => (
        ["knowledge_release", "combined"].includes(run?.run_type) && ["detected", "triaged"].includes(run?.status)
      )).length,
      failed: Number(overview.failed || 0),
      completed: Number(overview.completed || 0),
    });
  }

  function createLatestRequestController() {
    let nextToken = 0;
    let active = null;

    function finish(token) {
      if (active?.token !== token) return false;
      const onFinish = active.onFinish;
      active = null;
      onFinish?.();
      return true;
    }

    function cancel() {
      if (!active) return false;
      nextToken += 1;
      return finish(active.token);
    }

    async function run(request, handlers = {}) {
      cancel();
      const token = ++nextToken;
      active = { token, onFinish: handlers.onFinish };
      handlers.onStart?.();
      try {
        const value = await request();
        if (active?.token === token) handlers.onSuccess?.(value);
        return value;
      } catch (error) {
        if (active?.token === token) {
          if (typeof handlers.onError === "function") handlers.onError(error);
          else throw error;
        }
        return undefined;
      } finally {
        finish(token);
      }
    }

    return Object.freeze({
      run,
      cancel,
      isActive() { return Boolean(active); },
    });
  }

  function beginEvolutionRouteState(state, route, { loadsRuns = true } = {}) {
    const previousRun = String(state?.route?.run || "");
    const nextRun = String(route?.run || "");
    const scopeChanged = runsScopeKey(state?.route) !== runsScopeKey(route);
    state.route = route;
    state.loading = { overview: true, runs: Boolean(loadsRuns), detail: Boolean(nextRun) };
    state.errors = { overview: "", runs: "", detail: "", events: "" };
    if (previousRun !== nextRun) {
      state.selectedDetail = null;
      state.events = [];
    }
    if (scopeChanged) {
      state.runs = [];
      state.nextCursor = "";
    }
    return previousRun !== nextRun;
  }

  function restoreDismissedDialogFocus({ shouldRestore, dialog, trigger, schedule } = {}) {
    if (!shouldRestore || dialog || !trigger?.focus) return false;
    const enqueue = typeof schedule === "function" ? schedule : (callback) => callback();
    enqueue(() => trigger.focus());
    return true;
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

  function sortRunsForView(runs, view) {
    if (view !== "history") return sortRuns(runs);
    return Array.isArray(runs) ? [...runs].sort((left, right) => {
      const timeDelta = Date.parse(right?.created_at || "") - Date.parse(left?.created_at || "");
      if (Number.isFinite(timeDelta) && timeDelta !== 0) return timeDelta;
      return String(left?.run_id || "").localeCompare(String(right?.run_id || ""));
    }) : [];
  }

  function runTimestampForView(run, view) {
    return view === "history" ? run?.created_at : run?.updated_at;
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
    runStatusesForView,
    buildRunsQuery,
    detailTabForView,
    detailPresentation,
    routePatchForView,
    navigationPatchForDataset,
    runsScopeKey,
    queuePresentation,
    overviewStatusMetrics,
    createLatestRequestController,
    beginEvolutionRouteState,
    restoreDismissedDialogFocus,
    parseRoute,
    serializeRoute,
    groupPackages,
    selectCurrentPublished,
    sortRuns,
    sortRunsForView,
    runTimestampForView,
    scoreDelta,
    shouldHandleClick,
    activateDialog,
    bindDialogDismiss,
  });
}(globalThis));
