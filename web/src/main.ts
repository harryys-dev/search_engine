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
  content: string;
  filePath?: string;
  fileType?: string;
  score: number;
  snippet?: string;
  relevance: string;
}

const API_URL = "http://localhost:8080";

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

let currentPage = 1;
let currentQuery = "";

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
      suggestionDiv.innerHTML = `Возможно вы имели в виду: <a id="suggestionLink">${escapeHtml(data.suggestion)}</a>`;
      document
        .getElementById("suggestionLink")
        ?.addEventListener("click", () => {
          searchInput.value = data.suggestion!;
          doSearch(1);
        });
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

    const linkUrl = doc.url || doc.filePath;
    const displayUrl = doc.url || "";
    const titleHtml = linkUrl
      ? `<a href="${linkUrl}" target="_blank">${escapeHtml(doc.title)}</a>`
      : escapeHtml(doc.title);

    const relevanceLabels: Record<string, { text: string; cls: string }> = {
      high: { text: "Высокая релевантность", cls: "relevance-high" },
      medium: { text: "Средняя релевантность", cls: "relevance-medium" },
      low: { text: "Низкая релевантность", cls: "relevance-low" },
    };
    const rel = relevanceLabels[doc.relevance] || relevanceLabels["low"];

    div.innerHTML = `
            <div class="result-meta">
                <a href="${doc.url}" class="result-title-text">${doc.title}</a>
                <span class="result-score ${rel.cls}">${rel.text}</span>
                ${doc.fileType ? `<span class="file-type">${doc.fileType.toUpperCase()}</span>` : ""}
            </div>
            <div class="result-text">${doc.snippet || ""}</div>
        `;
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

function escapeHtml(text: string): string {
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
}
