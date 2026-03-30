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
const API_URL = 'http://localhost:8080';
const searchInput = document.getElementById('searchQuery');
const searchBtn = document.getElementById('searchBtn');
const resultsDiv = document.getElementById('resultsList');
const dropZone = document.getElementById('dropZone');
const fileInput = document.getElementById('fileInput');
const addBtn = document.getElementById('addBtn');
const crawlBtn = document.getElementById('crawlBtn');
const crawlStatus = document.getElementById('crawlStatus');
const crawlStatusText = document.getElementById('crawlStatusText');
const crawlProgress = document.getElementById('crawlProgress');
let crawlInterval = null;
searchInput.addEventListener('keypress', (e) => {
    if (e.key === 'Enter') {
        e.preventDefault();
        searchBtn.click();
    }
});
document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', (e) => {
        var _a;
        const target = e.target;
        const tabName = target.id.replace('tab-', '');
        document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
        target.classList.add('active');
        (_a = document.getElementById(`content-${tabName}`)) === null || _a === void 0 ? void 0 : _a.classList.add('active');
    });
});
searchBtn.addEventListener('click', () => __awaiter(void 0, void 0, void 0, function* () {
    const query = searchInput.value.trim();
    if (!query)
        return;
    try {
        const res = yield fetch(`${API_URL}/search?q=${encodeURIComponent(query)}`);
        if (!res.ok)
            throw new Error('Search failed');
        const data = yield res.json();
        resultsDiv.innerHTML = '';
        if (data.length === 0) {
            resultsDiv.innerHTML = '<div style="padding: 20px; text-align: center; color: #777;">Ничего не найдено</div>';
            return;
        }
        data.forEach(doc => {
            const div = document.createElement('div');
            div.className = 'result-item';
            // Bleve присылает готовый HTML-сниппет, или если нет - мы берем с бэкенда заглушку.
            let snippetHtml = doc.snippet || '';
            // Заголовок просто экранируем
            let titleHtml = escapeHtml(doc.title);
            // Создаем ссылку: используем URL для веб-страниц или filePath для файлов
            const linkUrl = doc.url || doc.filePath;
            if (linkUrl) {
                titleHtml = `<a href="${linkUrl}" target="_blank">${titleHtml}</a>`;
            }
            const relevanceLabels = {
                'high': { text: 'Высокая релевантность', class: 'relevance-high' },
                'medium': { text: 'Средняя релевантность', class: 'relevance-medium' },
                'low': { text: 'Низкая релевантность', class: 'relevance-low' }
            };
            const rel = relevanceLabels[doc.relevance] || relevanceLabels['low'];
            div.innerHTML = `
                <div class="result-title">
                    ${titleHtml}
                    <span class="result-score ${rel.class}">${rel.text}</span>
                    ${doc.fileType ? `<span class="file-type">${doc.fileType.toUpperCase()}</span>` : ''}
                </div>
                <div class="result-text">${snippetHtml}</div>
            `;
            resultsDiv.appendChild(div);
        });
    }
    catch (err) {
        console.error('Search error:', err);
        resultsDiv.innerHTML = '<div style="padding: 20px; text-align: center; color: #ea4335;">Ошибка поиска</div>';
    }
}));
dropZone.addEventListener('click', () => fileInput.click());
dropZone.addEventListener('dragover', (e) => {
    e.preventDefault();
    dropZone.classList.add('dragover');
});
dropZone.addEventListener('dragleave', () => dropZone.classList.remove('dragover'));
dropZone.addEventListener('drop', (e) => {
    var _a;
    e.preventDefault();
    dropZone.classList.remove('dragover');
    if ((_a = e.dataTransfer) === null || _a === void 0 ? void 0 : _a.files)
        handleFiles(e.dataTransfer.files);
});
fileInput.addEventListener('change', () => {
    if (fileInput.files)
        handleFiles(fileInput.files);
});
function handleFiles(files) {
    return __awaiter(this, void 0, void 0, function* () {
        const promises = Array.from(files).map((file) => __awaiter(this, void 0, void 0, function* () {
            const formData = new FormData();
            formData.append('file', file);
            try {
                const res = yield fetch(`${API_URL}/upload`, {
                    method: 'POST',
                    body: formData
                });
                if (res.ok) {
                    console.log(`Загружен: ${file.name}`);
                }
                else {
                    console.error(`Ошибка загрузки: ${file.name}`);
                }
            }
            catch (err) {
                console.error(`Ошибка для ${file.name}:`, err);
            }
        }));
        yield Promise.all(promises);
        alert("Файлы загружены!");
    });
}
addBtn.addEventListener('click', () => __awaiter(void 0, void 0, void 0, function* () {
    const title = document.getElementById('pageTitle').value.trim();
    const content = document.getElementById('pageContent').value.trim();
    if (!title || !content) {
        alert("Заполните поля");
        return;
    }
    try {
        const res = yield fetch(`${API_URL}/index`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: 0, title, content })
        });
        if (res.ok) {
            alert("Добавлено");
            document.getElementById('pageTitle').value = '';
            document.getElementById('pageContent').value = '';
        }
        else {
            alert("Ошибка добавления");
        }
    }
    catch (err) {
        console.error('Index error:', err);
        alert("Ошибка добавления");
    }
}));
crawlBtn === null || crawlBtn === void 0 ? void 0 : crawlBtn.addEventListener('click', () => __awaiter(void 0, void 0, void 0, function* () {
    const urlsText = document.getElementById('crawlUrls').value.trim();
    const maxPages = parseInt(document.getElementById('crawlMaxPages').value) || 20;
    const urls = urlsText ? urlsText.split('\n').map(u => u.trim()).filter(u => u.length > 0) : [];
    if (urls.length === 0) {
        alert("Пожалуйста, введите хотя бы один URL для сканирования.");
        return;
    }
    try {
        crawlBtn.disabled = true;
        crawlStatus.style.display = 'block';
        crawlStatusText.textContent = 'Запуск краулера...';
        const res = yield fetch(`${API_URL}/crawl`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url: urls[0], maxPages: maxPages })
        });
        if (res.ok) {
            startCrawlStatusPolling();
        }
        else {
            const error = yield res.text();
            crawlStatusText.textContent = `Ошибка: ${error}`;
            crawlBtn.disabled = false;
        }
    }
    catch (err) {
        console.error('Crawl error:', err);
        crawlStatusText.textContent = 'Ошибка запуска краулера';
        crawlBtn.disabled = false;
    }
}));
function startCrawlStatusPolling() {
    if (crawlInterval)
        clearInterval(crawlInterval);
    crawlInterval = window.setInterval(() => __awaiter(this, void 0, void 0, function* () {
        try {
            const res = yield fetch(`${API_URL}/crawl/status`);
            const status = yield res.json();
            crawlStatusText.textContent = status.message || 'Работает...';
            crawlProgress.textContent = `Страниц проиндексировано: ${status.pagesFound}`;
            if (!status.running) {
                if (crawlInterval)
                    clearInterval(crawlInterval);
                crawlInterval = null;
                crawlBtn.disabled = false;
                crawlStatusText.textContent = `✅ ${status.message}`;
            }
        }
        catch (err) {
            console.error('Status error:', err);
        }
    }), 1000);
}
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}
