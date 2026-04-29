interface SearchResponse {
  results: SearchResult[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
  searchTime: number;
  suggestion?: string;
  indexedPages: number;
}

interface SearchResult {
  id: number;
  url?: string;
  title: string;
  filePath?: string;
  fileType?: string;
  score: number;
  snippet?: string;
  relevance: string;
}

const API_URL =
  window.location.port === "5173"
    ? "http://127.0.0.1:8080"
    : window.location.origin;

const searchInput = document.getElementById("searchQuery") as HTMLInputElement;
const searchBtn = document.getElementById("searchBtn") as HTMLButtonElement;
const resultsDiv = document.getElementById("resultsList") as HTMLDivElement;
const paginationDiv = document.getElementById("pagination") as HTMLDivElement;
const suggestionDiv = document.getElementById("suggestion") as HTMLDivElement;
const indexedPagesSpan = document.getElementById(
  "indexedPages",
) as HTMLSpanElement;
const searchTimeSpan = document.getElementById("searchTime") as HTMLSpanElement;
const searchLoading = document.getElementById(
  "searchLoading",
) as HTMLDivElement;
const dropZone = document.getElementById("dropZone") as HTMLDivElement;
const fileInput = document.getElementById("fileInput") as HTMLInputElement;
const uploadStatus = document.getElementById("uploadStatus") as HTMLDivElement;
const themeToggle = document.getElementById("themeToggle") as HTMLButtonElement;

let currentPage = 1;
let currentQuery = "";

initTheme();
loadStats();

document.querySelectorAll(".tab-btn").forEach((btn) => {
  btn.addEventListener("click", () => {
    const target = btn as HTMLElement;
    const tabName = target.id.replace("tab-", "");

    document
      .querySelectorAll(".tab-btn")
      .forEach((b) => b.classList.remove("active"));
    document
      .querySelectorAll(".tab-content")
      .forEach((c) => c.classList.remove("active"));

    target.classList.add("active");
    document.getElementById(`content-${tabName}`)?.classList.add("active");
  });
});

searchInput.addEventListener("keypress", (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    doSearch(1);
  }
});

searchBtn.addEventListener("click", () => {
  doSearch(1);
});

async function loadStats() {
  try {
    const res = await fetch(`${API_URL}/stats`);
    const data = await res.json();
    indexedPagesSpan.textContent = `Проиндексировано страниц: ${data.indexedPages}`;
  } catch {
    indexedPagesSpan.textContent = "Проиндексировано страниц: —";
  }
}

async function doSearch(page: number) {
  const query = searchInput.value.trim();
  if (!query) return;

  currentPage = page;
  currentQuery = query;

  searchBtn.disabled = true;
  searchLoading.classList.add("active");
  resultsDiv.innerHTML = "";
  paginationDiv.innerHTML = "";
  searchTimeSpan.textContent = "";

  try {
    const params = new URLSearchParams({
      q: query,
      page: String(page),
      size: "10",
    });

    const res = await fetch(`${API_URL}/search?${params}`);
    if (!res.ok) throw new Error("Search failed");

    const data: SearchResponse = await res.json();

    indexedPagesSpan.textContent = `Проиндексировано страниц: ${data.indexedPages}`;
    searchTimeSpan.textContent = `${(data.searchTime * 1000).toFixed(0)} мс`;

    if (data.suggestion) {
      suggestionDiv.style.display = "block";
      suggestionDiv.textContent = "Возможно вы имели в виду: ";
      const suggestionLink = document.createElement("a");
      suggestionLink.id = "suggestionLink";
      suggestionLink.href = "#";
      suggestionLink.textContent = data.suggestion;
      suggestionLink.addEventListener("click", (event) => {
        event.preventDefault();
        searchInput.value = data.suggestion!;
        doSearch(1);
      });
      suggestionDiv.appendChild(suggestionLink);
    } else {
      suggestionDiv.style.display = "none";
    }

    renderResults(data.results);

    renderPagination(data.page, data.totalPages);

    await loadStats();
  } catch (err) {
    console.error("Search error:", err);
    resultsDiv.innerHTML =
      '<div class="empty-state"><div class="icon">⚠️</div><p>Ошибка поиска</p></div>';
    paginationDiv.innerHTML = "";
  } finally {
    searchBtn.disabled = false;
    searchLoading.classList.remove("active");
  }
}

function renderResults(results: SearchResult[]) {
  if (results.length === 0) {
    resultsDiv.innerHTML = `
            <div class="empty-state">
                <img src="/assets/gopher-cold-sweat.png" class="icon"></img>
                <p>Ничего не найдено</p>
            </div>`;
    return;
  }

  resultsDiv.innerHTML = "";
  results.forEach((doc) => {
    const div = document.createElement("div");
    div.className = "result-item";

    const relevanceLabels: Record<string, { text: string; cls: string }> = {
      high: { text: "Высокая релевантность", cls: "relevance-high" },
      medium: { text: "Средняя релевантность", cls: "relevance-medium" },
      low: { text: "Низкая релевантность", cls: "relevance-low" },
    };
    const rel = relevanceLabels[doc.relevance] || relevanceLabels["low"];

    const meta = document.createElement("div");
    meta.className = "result-meta";

    const titleNode = buildResultTitle(doc);
    titleNode.classList.add("result-title-text");
    meta.appendChild(titleNode);

    const score = document.createElement("span");
    score.className = `result-score ${rel.cls}`;
    score.textContent = rel.text;
    meta.appendChild(score);

    if (doc.fileType) {
      const fileType = document.createElement("span");
      fileType.className = "file-type";
      fileType.textContent = doc.fileType.toUpperCase();
      meta.appendChild(fileType);
    }

    const resultText = document.createElement("div");
    resultText.className = "result-text";
    renderSnippet(resultText, doc.snippet || "");

    div.appendChild(meta);
    div.appendChild(resultText);
    resultsDiv.appendChild(div);
  });
}

function renderPagination(currentPage: number, totalPages: number) {
  paginationDiv.innerHTML = "";

  if (totalPages <= 1) return;

  const prevBtn = document.createElement("button");
  prevBtn.textContent = "←";
  prevBtn.disabled = currentPage <= 1;
  prevBtn.addEventListener("click", () => doSearch(currentPage - 1));
  paginationDiv.appendChild(prevBtn);

  const maxVisible = 7;
  let startPage = Math.max(1, currentPage - 3);
  let endPage = Math.min(totalPages, startPage + maxVisible - 1);
  if (endPage - startPage < maxVisible - 1) {
    startPage = Math.max(1, endPage - maxVisible + 1);
  }

  if (startPage > 1) {
    addPageButton(1);
    if (startPage > 2) {
      const dots = document.createElement("span");
      dots.textContent = "...";
      dots.style.padding = "8px 4px";
      dots.style.color = "#999";
      paginationDiv.appendChild(dots);
    }
  }

  for (let i = startPage; i <= endPage; i++) {
    addPageButton(i);
  }

  if (endPage < totalPages) {
    if (endPage < totalPages - 1) {
      const dots = document.createElement("span");
      dots.textContent = "...";
      dots.style.padding = "8px 4px";
      dots.style.color = "#999";
      paginationDiv.appendChild(dots);
    }
    addPageButton(totalPages);
  }

  const nextBtn = document.createElement("button");
  nextBtn.textContent = "→";
  nextBtn.disabled = currentPage >= totalPages;
  nextBtn.addEventListener("click", () => doSearch(currentPage + 1));
  paginationDiv.appendChild(nextBtn);
}

function addPageButton(pageNum: number) {
  const btn = document.createElement("button");
  btn.textContent = String(pageNum);
  if (pageNum === currentPage) {
    btn.classList.add("active");
  }
  btn.addEventListener("click", () => doSearch(pageNum));
  paginationDiv.appendChild(btn);
}

dropZone.addEventListener("click", () => fileInput.click());
dropZone.addEventListener("dragover", (e) => {
  e.preventDefault();
  dropZone.classList.add("dragover");
});
dropZone.addEventListener("dragleave", () =>
  dropZone.classList.remove("dragover"),
);
dropZone.addEventListener("drop", (e) => {
  e.preventDefault();
  dropZone.classList.remove("dragover");
  if (e.dataTransfer?.files) handleFiles(e.dataTransfer.files);
});
fileInput.addEventListener("change", () => {
  if (fileInput.files) handleFiles(fileInput.files);
});

async function handleFiles(files: FileList): Promise<void> {
  const allowed = [".pdf", ".html", ".htm"];
  const validFiles = Array.from(files).filter((f) => {
    const ext = "." + f.name.split(".").pop()?.toLowerCase();
    return allowed.includes(ext);
  });

  if (validFiles.length === 0) {
    uploadStatus.style.display = "block";
    uploadStatus.textContent = "⚠️ Поддерживаются только файлы .html и .pdf";
    uploadStatus.style.color = "#EA4335";
    return;
  }

  uploadStatus.style.display = "block";
  uploadStatus.style.color = "#34A853";
  uploadStatus.textContent = `Загрузка ${validFiles.length} файл(ов)...`;

  let success = 0;
  for (const file of validFiles) {
    const formData = new FormData();
    formData.append("file", file);
    try {
      const res = await fetch(`${API_URL}/upload`, {
        method: "POST",
        body: formData,
      });
      if (res.ok) success++;
    } catch (err) {
      console.error(`Upload error for ${file.name}:`, err);
    }
  }

  uploadStatus.textContent = `✅ Загружено: ${success} из ${validFiles.length} файл(ов)`;
  await loadStats();
  setTimeout(() => {
    uploadStatus.style.display = "none";
  }, 5000);
}

function buildResultTitle(doc: SearchResult): HTMLElement {
  const href = normalizeResultHref(doc.url || doc.filePath);
  if (!href) {
    const span = document.createElement("span");
    span.textContent = doc.title;
    return span;
  }

  const link = document.createElement("a");
  link.href = href;
  link.target = "_blank";
  link.rel = "noopener noreferrer";
  link.textContent = doc.title;
  return link;
}

function normalizeResultHref(raw?: string): string | null {
  if (!raw) return null;
  if (raw.startsWith("/files/")) return raw;

  try {
    const url = new URL(raw);
    if (url.protocol === "http:" || url.protocol === "https:") {
      return url.toString();
    }
  } catch {
    return null;
  }

  return null;
}

function initTheme() {
  const saved = localStorage.getItem("theme");
  applyTheme(getPreferredTheme(saved));

  themeToggle.addEventListener("click", () => {
    const current = document.documentElement.getAttribute("data-theme");
    applyTheme(current === "dark" ? "light" : "dark");
  });
}

function getPreferredTheme(saved: string | null): "dark" | "light" {
  if (saved === "dark" || saved === "light") {
    return saved;
  }

  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

function applyTheme(theme: "dark" | "light") {
  document.documentElement.setAttribute("data-theme", theme);
  themeToggle.textContent = theme === "dark" ? "☀️" : "🌙";
  localStorage.setItem("theme", theme);
}

function renderSnippet(container: HTMLElement, snippet: string) {
  container.replaceChildren();

  const parts = snippet.split(/(<mark>|<\/mark>)/g);
  let inMark = false;

  for (const part of parts) {
    if (part === "<mark>") {
      inMark = true;
      continue;
    }
    if (part === "</mark>") {
      inMark = false;
      continue;
    }
    if (!part) {
      continue;
    }

    const text = decodeHtmlEntities(part);
    if (!text) {
      continue;
    }

    if (inMark) {
      const mark = document.createElement("mark");
      mark.textContent = text;
      container.appendChild(mark);
      continue;
    }

    container.appendChild(document.createTextNode(text));
  }
}

function decodeHtmlEntities(text: string): string {
  const textarea = document.createElement("textarea");
  textarea.innerHTML = text;
  return textarea.value;
}
