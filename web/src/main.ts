interface SearchDocument {
    id: number;
    url?: string; // URL для веб-страниц
    title: string;
    content: string;
    filePath?: string; // Путь для загруженных файлов
    fileType?: string;
}

interface SearchResult extends SearchDocument {
    score: number;
    snippet?: string;
    relevance: string;
}

const API_URL = 'http://localhost:8080';

const searchInput = document.getElementById('searchQuery') as HTMLInputElement;
const searchBtn = document.getElementById('searchBtn') as HTMLButtonElement;
const resultsDiv = document.getElementById('resultsList') as HTMLDivElement;
const dropZone = document.getElementById('dropZone') as HTMLDivElement;
const fileInput = document.getElementById('fileInput') as HTMLInputElement;
const addBtn = document.getElementById('addBtn') as HTMLButtonElement;
const crawlBtn = document.getElementById('crawlBtn') as HTMLButtonElement;
const crawlStatus = document.getElementById('crawlStatus') as HTMLDivElement;
const crawlStatusText = document.getElementById('crawlStatusText') as HTMLDivElement;
const crawlProgress = document.getElementById('crawlProgress') as HTMLDivElement;

let crawlInterval: number | null = null;

searchInput.addEventListener('keypress', (e) => {
    if (e.key === 'Enter') {
        e.preventDefault();
        searchBtn.click();
    }
});

document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', (e) => {
        const target = e.target as HTMLElement;
        const tabName = target.id.replace('tab-', '');

        document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));

        target.classList.add('active');
        document.getElementById(`content-${tabName}`)?.classList.add('active');
    });
});

searchBtn.addEventListener('click', async () => {
    const query = searchInput.value.trim();
    if (!query) return;

    try {
        const res = await fetch(`${API_URL}/search?q=${encodeURIComponent(query)}`);
        if (!res.ok) throw new Error('Search failed');
        
        const data: SearchResult[] = await res.json();

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

            const relevanceLabels: Record<string, {text: string, class: string}> = {
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
    } catch (err) {
        console.error('Search error:', err);
        resultsDiv.innerHTML = '<div style="padding: 20px; text-align: center; color: #ea4335;">Ошибка поиска</div>';
    }
});

dropZone.addEventListener('click', () => fileInput.click());
dropZone.addEventListener('dragover', (e) => {
    e.preventDefault();
    dropZone.classList.add('dragover');
});
dropZone.addEventListener('dragleave', () => dropZone.classList.remove('dragover'));
dropZone.addEventListener('drop', (e) => {
    e.preventDefault();
    dropZone.classList.remove('dragover');
    if (e.dataTransfer?.files) handleFiles(e.dataTransfer.files);
});
fileInput.addEventListener('change', () => {
    if (fileInput.files) handleFiles(fileInput.files);
});

async function handleFiles(files: FileList): Promise<void> {
    const promises = Array.from(files).map(async (file) => {
        const formData = new FormData();
        formData.append('file', file);
        try {
            const res = await fetch(`${API_URL}/upload`, {
                method: 'POST',
                body: formData
            });
            if (res.ok) {
                console.log(`Загружен: ${file.name}`);
            } else {
                console.error(`Ошибка загрузки: ${file.name}`);
            }
        } catch (err) {
            console.error(`Ошибка для ${file.name}:`, err);
        }
    });
    
    await Promise.all(promises);
    alert("Файлы загружены!");
}

addBtn.addEventListener('click', async () => {
    const title = (document.getElementById('pageTitle') as HTMLInputElement).value.trim();
    const content = (document.getElementById('pageContent') as HTMLTextAreaElement).value.trim();

    if (!title || !content) {
        alert("Заполните поля");
        return;
    }

    try {
        const res = await fetch(`${API_URL}/index`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: 0, title, content })
        });

        if (res.ok) {
            alert("Добавлено");
            (document.getElementById('pageTitle') as HTMLInputElement).value = '';
            (document.getElementById('pageContent') as HTMLTextAreaElement).value = '';
        } else {
            alert("Ошибка добавления");
        }
    } catch (err) {
        console.error('Index error:', err);
        alert("Ошибка добавления");
    }
});

crawlBtn?.addEventListener('click', async () => {
    const urlsText = (document.getElementById('crawlUrls') as HTMLTextAreaElement).value.trim();
    const maxPages = parseInt((document.getElementById('crawlMaxPages') as HTMLInputElement).value) || 20;
    
    const urls = urlsText ? urlsText.split('\n').map(u => u.trim()).filter(u => u.length > 0) : [];

    if (urls.length === 0) {
        alert("Пожалуйста, введите хотя бы один URL для сканирования.");
        return;
    }
    
    try {
        crawlBtn.disabled = true;
        crawlStatus.style.display = 'block';
        crawlStatusText.textContent = 'Запуск краулера...';
        
        const res = await fetch(`${API_URL}/crawl`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url: urls[0], maxPages: maxPages })
        });
        
        if (res.ok) {
            startCrawlStatusPolling();
        } else {
            const error = await res.text();
            crawlStatusText.textContent = `Ошибка: ${error}`;
            crawlBtn.disabled = false;
        }
    } catch (err) {
        console.error('Crawl error:', err);
        crawlStatusText.textContent = 'Ошибка запуска краулера';
        crawlBtn.disabled = false;
    }
});

function startCrawlStatusPolling() {
    if (crawlInterval) clearInterval(crawlInterval);
    
    crawlInterval = window.setInterval(async () => {
        try {
            const res = await fetch(`${API_URL}/crawl/status`);
            const status = await res.json();
            
            crawlStatusText.textContent = status.message || 'Работает...';
            crawlProgress.textContent = `Страниц проиндексировано: ${status.pagesFound}`;
            
            if (!status.running) {
                if (crawlInterval) clearInterval(crawlInterval);
                crawlInterval = null;
                crawlBtn.disabled = false;
                crawlStatusText.textContent = `✅ ${status.message}`;
            }
        } catch (err) {
            console.error('Status error:', err);
        }
    }, 1000);
}

function escapeHtml(text: string): string {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}