package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"search_engine/internal/crawler"
	"search_engine/internal/engine"
	"search_engine/internal/models"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type CrawlRequest struct {
	URL      string `json:"url"`
	MaxPages int    `json:"maxPages"`
}

var (
	searchEngine  = engine.NewEngine()
	idCounter     int64
	crawlerStatus = struct {
		sync.RWMutex
		Running    bool   `json:"running"`
		PagesFound int    `json:"pagesFound"`
		Message    string `json:"message"`
	}{}
)

func main() {
	if err := os.MkdirAll("uploads", os.ModePerm); err != nil {
		log.Fatalf("Failed to create uploads directory: %v", err)
	}

	http.HandleFunc("/search", handleSearch)
	http.HandleFunc("/index", handleIndex)
	http.HandleFunc("/upload", handleUpload)
	http.HandleFunc("/crawl", handleCrawl)
	http.HandleFunc("/crawl/status", handleCrawlStatus)
	http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir("./uploads"))))
	http.Handle("/", http.StripPrefix("/", http.FileServer(http.Dir("./web"))))

	server := &http.Server{
		Addr:         ":8080",
		Handler:      corsMiddleware(http.DefaultServeMux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	fmt.Println("Сервер запущен на http://localhost:8080")
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func cleanHTML(raw string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return ""
	}

	doc.Find("script, style, nav, header, footer, aside, .sidebar, .menu, table.infobox, .reference, .mw-editsection").Remove()

	var sb strings.Builder
	doc.Find("p").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if len(text) > 0 {
			sb.WriteString(text)
			sb.WriteString(" ")
		}
	})

	text := sb.String()

	if len(text) < 100 {
		text = doc.Find("body").Text()
	}

	wikiPatterns := []string{
		`\[\s*(править|правка|edit)\s*\|?\s*(код|source|wiki)?\s*\]`,
		`\[\s*\d+\s*\]`,
		`\[\s*править\s*код\s*\]`,
		`\[\s*скрыть\s*\]`,
		`\[\s*показать\s*\]`,
		`Материал из Википедии\s*—\s*свободной энциклопедии`,
		`Текущая версия страницы пока не проверялась опытными участниками[^.]*\.`,
		`Перейти к навигации`,
		`Перейти к поиску`,
		`У этого термина существуют и другие значения[^.]*\.`,
		`Запрос «[^»]*» перенаправляется сюда[^.]*\.`,
		`См\. также[^.]*\.`,
		`Эта статья[^.]*\.`,
	}
	for _, pattern := range wikiPatterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		text = re.ReplaceAllString(text, " ")
	}

	multiSpaceRegex := regexp.MustCompile(`\s+`)
	text = multiSpaceRegex.ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

func extractPDFText(filePath string) (string, error) {
	cmd := exec.Command("pdftotext", "-layout", filePath, "-")
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftotext error: %v, stderr: %s", err, stderr.String())
	}

	text := out.String()
	text = strings.Join(strings.Fields(text), " ")
	return text, nil
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(20 << 20); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File error", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(handler.Filename))
	safeFilename := filepath.Base(handler.Filename)
	titleWithoutExt := strings.TrimSuffix(safeFilename, ext)
	dstPath := filepath.Join("uploads", safeFilename)

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(dstPath, fileBytes, 0644); err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	var content string
	switch ext {
	case ".pdf":
		txt, err := extractPDFText(dstPath)
		if err != nil {
			content = "PDF read error"
			log.Printf("PDF extraction error: %v", err)
		} else {
			content = txt
		}
	case ".html", ".htm":
		content = cleanHTML(string(fileBytes))
	default:
		content = string(fileBytes)
	}

	fileType := strings.TrimPrefix(ext, ".")
	if fileType == "htm" {
		fileType = "html"
	}
	if fileType == "" {
		fileType = "txt"
	}

	newID := int(atomic.AddInt64(&idCounter, 1))
	doc := models.Document{
		ID:       newID,
		Title:    titleWithoutExt,
		Content:  content,
		FilePath: "/files/" + safeFilename,
		FileType: fileType,
	}

	searchEngine.Index(doc)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(doc); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Query parameter 'q' required", http.StatusBadRequest)
		return
	}

	results := searchEngine.Search(query)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var doc models.Document
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if doc.ID == 0 {
		doc.ID = int(atomic.AddInt64(&idCounter, 1))
	}
	doc.Content = cleanHTML(doc.Content)
	searchEngine.Index(doc)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(doc); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

var defaultSeedURLs = []string{
	"https://ru.wikipedia.org/wiki/Поисковая_система",
	"https://ru.wikipedia.org/wiki/Информатика",
}

func handleCrawl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	crawlerStatus.RLock()
	if crawlerStatus.Running {
		crawlerStatus.RUnlock()
		http.Error(w, "Crawl is already in progress", http.StatusConflict)
		return
	}
	crawlerStatus.RUnlock()

	var req CrawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	go runCrawler(req)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"message": "Crawl started"})
}

func runCrawler(req CrawlRequest) {
	crawlerStatus.Lock()
	crawlerStatus.Running = true
	crawlerStatus.PagesFound = 0
	crawlerStatus.Message = "Crawl started for " + req.URL
	crawlerStatus.Unlock()

	defer func() {
		crawlerStatus.Lock()
		crawlerStatus.Running = false
		crawlerStatus.Message = "Crawl finished. Found " + fmt.Sprintf("%d", crawlerStatus.PagesFound) + " new pages."
		crawlerStatus.Unlock()
	}()

	cfg := crawler.DefaultConfig()
	if req.MaxPages > 0 {
		cfg.MaxPages = req.MaxPages
	}
	// Передаем searchEngine в краулер
	c := crawler.New(cfg, searchEngine)

	c.OnPage(func(p crawler.Page) {
		doc := models.Document{
			ID:          int(atomic.AddInt64(&idCounter, 1)),
			URL:         p.URL,
			Title:       p.Title,
			Content:     p.Content,
			ContentHash: p.ContentHash,
			FileType:    "web",
		}
		searchEngine.Index(doc)

		crawlerStatus.Lock()
		crawlerStatus.PagesFound++
		crawlerStatus.Unlock()
	})

	log.Println("Starting crawl from:", req.URL)
	c.Crawl([]string{req.URL})
	log.Println("Crawl finished.")
}

func handleCrawlStatus(w http.ResponseWriter, r *http.Request) {
	crawlerStatus.RLock()
	defer crawlerStatus.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"running":    crawlerStatus.Running,
		"pagesFound": crawlerStatus.PagesFound,
		"message":    crawlerStatus.Message,
	})
}

func extractHosts(urls []string) []string {
	hosts := make(map[string]bool)
	for _, u := range urls {
		if strings.Contains(u, "://") {
			parts := strings.Split(u, "/")
			if len(parts) >= 3 {
				host := parts[2]
				hosts[host] = true
				if strings.HasPrefix(host, "www.") {
					hosts[host[4:]] = true
				} else {
					hosts["www."+host] = true
				}
			}
		}
	}
	result := make([]string, 0, len(hosts))
	for h := range hosts {
		result = append(result, h)
	}
	return result
}
