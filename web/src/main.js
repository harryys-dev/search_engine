"use strict";
var __awaiter = (this && this.__awaiter) || function (thisArg, _arguments, P, generator) {
    function adopt(value) { return value instanceof P ? value : new P(function (resolve) { resolve(value); }); }
    return new (P || (P = Promise))(function (resolve, reject) {
        function fulfilled(value) { try { step(generator.next(value)); } catch (e) { reject(e); } }
        function rejected(value) { try { step(generator["throw"](value)); } catch (e) { reject(e); } }
        function step(result) { result.done ? resolve(result.value) : adopt(result.value).then(fulfilled, rejected); }
        step((generator = generator.apply(thisArg, _arguments || [])).next());
    });
};
const API_URL = "http://localhost:8080";
const searchInput = document.getElementById("searchQuery");
const searchBtn = document.getElementById("searchBtn");
const resultsDiv = document.getElementById("resultsList");
const paginationDiv = document.getElementById("pagination");
const suggestionDiv = document.getElementById("suggestion");
const indexedPagesSpan = document.getElementById("indexedPages");
const searchTimeSpan = document.getElementById("searchTime");
const searchLoading = document.getElementById("searchLoading");
const dropZone = document.getElementById("dropZone");
const fileInput = document.getElementById("fileInput");
const uploadStatus = document.getElementById("uploadStatus");
let currentPage = 1;
let currentQuery = "";
// Загружаем статистику при старте
loadStats();
// Tabs
document.querySelectorAll(".tab-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
        var _a;
        const target = btn;
        const tabName = target.id.replace("tab-", "");
        document
            .querySelectorAll(".tab-btn")
            .forEach((b) => b.classList.remove("active"));
        document
            .querySelectorAll(".tab-content")
            .forEach((c) => c.classList.remove("active"));
        target.classList.add("active");
        (_a = document.getElementById(`content-${tabName}`)) === null || _a === void 0 ? void 0 : _a.classList.add("active");
    });
});
// Поиск по Enter
searchInput.addEventListener("keypress", (e) => {
    if (e.key === "Enter") {
        e.preventDefault();
        doSearch(1);
    }
});
searchBtn.addEventListener("click", () => {
    doSearch(1);
});
function loadStats() {
    return __awaiter(this, void 0, void 0, function* () {
        try {
            const res = yield fetch(`${API_URL}/stats`);
            const data = yield res.json();
            indexedPagesSpan.textContent = `Проиндексировано страниц: ${data.indexedPages}`;
        }
        catch (_a) {
            indexedPagesSpan.textContent = "Проиндексировано страниц: —";
        }
    });
}
function doSearch(page) {
    return __awaiter(this, void 0, void 0, function* () {
        var _a;
        const query = searchInput.value.trim();
        if (!query)
            return;
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
            const res = yield fetch(`${API_URL}/search?${params}`);
            if (!res.ok)
                throw new Error("Search failed");
            const data = yield res.json();
            // Обновляем статистику из ответа
            indexedPagesSpan.textContent = `Проиндексировано страниц: ${data.indexedPages}`;
            searchTimeSpan.textContent = `${(data.searchTime * 1000).toFixed(0)} мс`;
            // Подсказка
            if (data.suggestion) {
                suggestionDiv.style.display = "block";
                suggestionDiv.innerHTML = `Возможно вы имели в виду: <a id="suggestionLink">${escapeHtml(data.suggestion)}</a>`;
                (_a = document
                    .getElementById("suggestionLink")) === null || _a === void 0 ? void 0 : _a.addEventListener("click", () => {
                    searchInput.value = data.suggestion;
                    doSearch(1);
                });
            }
            else {
                suggestionDiv.style.display = "none";
            }
            // Результаты
            renderResults(data.results);
            // Пагинация
            renderPagination(data.page, data.totalPages);
            // Обновляем stats
            yield loadStats();
        }
        catch (err) {
            console.error("Search error:", err);
            resultsDiv.innerHTML =
                '<div class="empty-state"><div class="icon">⚠️</div><p>Ошибка поиска</p></div>';
            paginationDiv.innerHTML = "";
        }
        finally {
            searchBtn.disabled = false;
            searchLoading.classList.remove("active");
        }
    });
}
function renderResults(results) {
    if (results.length === 0) {
        resultsDiv.innerHTML = `
            <div class="empty-state">
                <div class="icon">🔍</div>
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
        const relevanceLabels = {
            high: { text: "Высокая", cls: "relevance-high" },
            medium: { text: "Средняя", cls: "relevance-medium" },
            low: { text: "Низкая", cls: "relevance-low" },
        };
        const rel = relevanceLabels[doc.relevance] || relevanceLabels["low"];
        div.innerHTML = `
            ${displayUrl ? `<div class="result-url"><a href="${linkUrl}" target="_blank">${escapeHtml(displayUrl)}</a></div>` : ""}
            <div class="result-title">${titleHtml}</div>
            <div class="result-meta">
                <span class="result-score ${rel.cls}">${rel.text}</span>
                ${doc.fileType ? `<span class="file-type">${doc.fileType.toUpperCase()}</span>` : ""}
            </div>
            <div class="result-text">${doc.snippet || ""}</div>
        `;
        resultsDiv.appendChild(div);
    });
}
function renderPagination(currentPage, totalPages) {
    paginationDiv.innerHTML = "";
    if (totalPages <= 1)
        return;
    // Кнопка "назад"
    const prevBtn = document.createElement("button");
    prevBtn.textContent = "←";
    prevBtn.disabled = currentPage <= 1;
    prevBtn.addEventListener("click", () => doSearch(currentPage - 1));
    paginationDiv.appendChild(prevBtn);
    // Номера страниц
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
    // Кнопка "вперёд"
    const nextBtn = document.createElement("button");
    nextBtn.textContent = "→";
    nextBtn.disabled = currentPage >= totalPages;
    nextBtn.addEventListener("click", () => doSearch(currentPage + 1));
    paginationDiv.appendChild(nextBtn);
}
function addPageButton(pageNum) {
    const btn = document.createElement("button");
    btn.textContent = String(pageNum);
    if (pageNum === currentPage) {
        btn.classList.add("active");
    }
    btn.addEventListener("click", () => doSearch(pageNum));
    paginationDiv.appendChild(btn);
}
// File upload
dropZone.addEventListener("click", () => fileInput.click());
dropZone.addEventListener("dragover", (e) => {
    e.preventDefault();
    dropZone.classList.add("dragover");
});
dropZone.addEventListener("dragleave", () => dropZone.classList.remove("dragover"));
dropZone.addEventListener("drop", (e) => {
    var _a;
    e.preventDefault();
    dropZone.classList.remove("dragover");
    if ((_a = e.dataTransfer) === null || _a === void 0 ? void 0 : _a.files)
        handleFiles(e.dataTransfer.files);
});
fileInput.addEventListener("change", () => {
    if (fileInput.files)
        handleFiles(fileInput.files);
});
function handleFiles(files) {
    return __awaiter(this, void 0, void 0, function* () {
        const allowed = [".pdf", ".html", ".htm"];
        const validFiles = Array.from(files).filter((f) => {
            var _a;
            const ext = "." + ((_a = f.name.split(".").pop()) === null || _a === void 0 ? void 0 : _a.toLowerCase());
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
                const res = yield fetch(`${API_URL}/upload`, {
                    method: "POST",
                    body: formData,
                });
                if (res.ok)
                    success++;
            }
            catch (err) {
                console.error(`Upload error for ${file.name}:`, err);
            }
        }
        uploadStatus.textContent = `✅ Загружено: ${success} из ${validFiles.length} файл(ов)`;
        yield loadStats();
        setTimeout(() => {
            uploadStatus.style.display = "none";
        }, 5000);
    });
}
function escapeHtml(text) {
    const div = document.createElement("div");
    div.textContent = text;
    return div.innerHTML;
}
