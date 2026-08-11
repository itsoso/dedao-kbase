const knowledgePackagesPath = "/knowledge/packages";

function decodeRouteSegment(value) {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

export function knowledgeBookPath(bookID) {
  const id = String(bookID || "").trim();
  return id ? `${knowledgePackagesPath}/${encodeURIComponent(id)}` : knowledgePackagesPath;
}

export function knowledgeChapterPath(bookID, chapterID) {
  const id = String(chapterID || "").trim();
  return id ? `${knowledgeBookPath(bookID)}/chapters/${encodeURIComponent(id)}` : knowledgeBookPath(bookID);
}

export function knowledgeResultPath(bookID, kind, resultID) {
  const cleanKind = String(kind || "").trim();
  const id = String(resultID || "").trim();
  return cleanKind && id
    ? `${knowledgeBookPath(bookID)}/results/${encodeURIComponent(cleanKind)}/${encodeURIComponent(id)}`
    : knowledgeBookPath(bookID);
}

export function knowledgeResourceFromPath(pathname) {
  const prefix = `${knowledgePackagesPath}/`;
  if (!String(pathname || "").startsWith(prefix)) {
    return null;
  }
  const parts = String(pathname).slice(prefix.length).split("/").filter(Boolean);
  const bookID = decodeRouteSegment(parts[0] || "");
  if (!bookID) {
    return null;
  }
  if (parts.length === 1) {
    return { type: "book", bookID, resourceID: "", kind: "" };
  }
  if (parts.length === 3 && parts[1] === "chapters") {
    return { type: "chapter", bookID, resourceID: decodeRouteSegment(parts[2]), kind: "chapter" };
  }
  if (parts.length === 4 && parts[1] === "results") {
    return {
      type: "result",
      bookID,
      resourceID: decodeRouteSegment(parts[3]),
      kind: decodeRouteSegment(parts[2]),
    };
  }
  return null;
}

export function knowledgeBookForRoute(books, resource) {
  const bookID = String(resource?.bookID || "").trim();
  return bookID
    ? (Array.isArray(books) ? books : []).find((book) => String(book?.book_id || "") === bookID) || null
    : null;
}

export function knowledgeResultRouteID(result) {
  return String(
    result?.id || result?.claim_id || result?.chunk_id || result?.citation_id || result?.chapter_id || "",
  ).trim();
}

export function knowledgeResultFromPackage(pkg, resource) {
  const kind = String(resource?.kind || "").trim();
  const resourceID = String(resource?.resourceID || "").trim();
  if (!resourceID) {
    return null;
  }
  if (kind === "chunk") {
    const chunk = (pkg?.chunks || []).find((item) => String(item.chunk_id || "") === resourceID);
    if (!chunk) {
      return null;
    }
    const chapter = (pkg?.chapters || []).find((item) => item.chapter_id === chunk.chapter_id);
    return {
      kind,
      id: resourceID,
      chunk_id: resourceID,
      title: chapter?.title || chunk.chapter_id || resourceID,
      snippet: chunk.text || "",
    };
  }
  if (kind === "claim") {
    const claim = (pkg?.claims || []).find((item) => String(item.claim_id || "") === resourceID);
    return claim ? {
      kind,
      id: resourceID,
      claim_id: resourceID,
      title: claim.title || resourceID,
      snippet: claim.summary || claim.body || "",
    } : null;
  }
  if (kind === "citation") {
    const citation = (pkg?.citations || []).find((item) => String(item.citation_id || "") === resourceID);
    return citation ? {
      kind,
      id: resourceID,
      citation_id: resourceID,
      title: `引用 ${resourceID}`,
      snippet: citation.note || citation.anchor || [citation.chapter_id, citation.chunk_id].filter(Boolean).join(" · "),
    } : null;
  }
  return null;
}

export function shouldHandleKnowledgeClick(event) {
  return event?.button === 0
    && !event.metaKey
    && !event.ctrlKey
    && !event.shiftKey
    && !event.altKey;
}

export function knowledgeResourceTargetSelector(resource) {
  if (resource?.type === "chapter") {
    return "[data-chapter-index].active";
  }
  if (resource?.type === "result") {
    return "[data-result-index].active";
  }
  return "";
}

export function knowledgeSearchIsCurrent(sequence, currentSequence, routeBookID, currentRouteBookID) {
  return sequence === currentSequence && String(routeBookID || "") === String(currentRouteBookID || "");
}
