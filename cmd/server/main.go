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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"search_engine/internal/crawler"
	"search_engine/internal/engine"
	"search_engine/internal/models"

	"github.com/PuerkitoBio/goquery"
)

type CrawlerConfig struct {
	SeedURLs     []string `json:"seed_urls"`
	MaxPages     int      `json:"max_pages"`
	MaxDepth     int      `json:"max_depth"`
	DelayMs      int      `json:"delay_ms"`
	Parallelism  int      `json:"parallelism"`
	AllowedHosts []string `json:"allowed_hosts"`
	UserAgent    string   `json:"user_agent"`
	AutoStart    bool     `json:"auto_start"`
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

	crawlerCfg := loadCrawlerConfig()

	if crawlerCfg.AutoStart && len(crawlerCfg.SeedURLs) > 0 {
		go autoStartCrawler(crawlerCfg)
	}

	http.HandleFunc("/search", handleSearch)
	http.HandleFunc("/stats", handleStats)
	http.HandleFunc("/upload", handleUpload)
	http.HandleFunc("/crawl", handleCrawl)
	http.HandleFunc("/crawl/status", handleCrawlStatus)
	http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir("./uploads"))))
	http.Handle("/", http.StripPrefix("/", http.FileServer(http.Dir("./web/dist"))))

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

func loadCrawlerConfig() CrawlerConfig {
	data, err := os.ReadFile("crawler.json")
	if err != nil {
		log.Println("crawler.json not found, using defaults")
		return CrawlerConfig{
			SeedURLs:    []string{},
			MaxPages:    100,
			MaxDepth:    4,
			DelayMs:     500,
			Parallelism: 5,
			UserAgent:   "GoSearchEngine/1.0",
			AutoStart:   false,
		}
	}

	var cfg CrawlerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("Error parsing crawler.json: %v", err)
		return CrawlerConfig{AutoStart: false}
	}

	if cfg.MaxPages == 0 {
		cfg.MaxPages = 100
	}
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = 4
	}
	if cfg.DelayMs == 0 {
		cfg.DelayMs = 500
	}
	if cfg.Parallelism == 0 {
		cfg.Parallelism = 5
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "GoSearchEngine/1.0"
	}

	log.Printf("Loaded crawler config: %d seed URLs, max %d pages, depth %d, parallelism %d",
		len(cfg.SeedURLs), cfg.MaxPages, cfg.MaxDepth, cfg.Parallelism)
	return cfg
}

func autoStartCrawler(cfg CrawlerConfig) {
	time.Sleep(1 * time.Second)

	crawlerStatus.Lock()
	crawlerStatus.Running = true
	crawlerStatus.Message = "Автоматический краулинг запущен..."
	crawlerStatus.Unlock()

	defer func() {
		crawlerStatus.Lock()
		crawlerStatus.Running = false
		crawlerStatus.Message = fmt.Sprintf("Краулинг завершён. Проиндексировано: %d страниц.", crawlerStatus.PagesFound)
		crawlerStatus.Unlock()
	}()

	crawlerCfg := crawler.Config{
		MaxPages:     cfg.MaxPages,
		MaxDepth:     cfg.MaxDepth,
		Delay:        time.Duration(cfg.DelayMs) * time.Millisecond,
		UserAgent:    cfg.UserAgent,
		AllowedHosts: cfg.AllowedHosts,
	}

	c := crawler.New(crawlerCfg, searchEngine)

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

	log.Printf("Auto-crawl started from %d URLs", len(cfg.SeedURLs))
	c.Crawl(cfg.SeedURLs)
	log.Println("Auto-crawl finished.")
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

func handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Query parameter 'q' required", http.StatusBadRequest)
		return
	}

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if val, err := strconv.Atoi(p); err == nil && val > 0 {
			page = val
		}
	}

	size := 10
	if s := r.URL.Query().Get("size"); s != "" {
		if val, err := strconv.Atoi(s); err == nil && val > 0 && val <= 50 {
			size = val
		}
	}

	response := searchEngine.SearchPaginated(query, page, size)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	crawlerStatus.RLock()
	defer crawlerStatus.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(models.StatsResponse{
		IndexedPages: searchEngine.DocCount(),
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

	if err := os.WriteFile(dstPath, fileBytes, 0o644); err != nil {
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
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"title":    doc.Title,
		"fileType": doc.FileType,
	}); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
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

	cfg := loadCrawlerConfig()

	var req struct {
		URL      string `json:"url"`
		MaxPages int    `json:"maxPages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	seedURLs := cfg.SeedURLs
	if req.URL != "" {
		seedURLs = append([]string{req.URL}, seedURLs...)
	}

	if len(seedURLs) == 0 {
		http.Error(w, "No URLs to crawl", http.StatusBadRequest)
		return
	}

	go runCrawlerWithConfig(seedURLs, req.MaxPages, cfg)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"message": "Crawl started"})
}

func runCrawlerWithConfig(seedURLs []string, reqMaxPages int, cfg CrawlerConfig) {
	crawlerStatus.Lock()
	crawlerStatus.Running = true
	crawlerStatus.PagesFound = 0
	crawlerStatus.Message = "Краулинг запущен..."
	crawlerStatus.Unlock()

	defer func() {
		crawlerStatus.Lock()
		crawlerStatus.Running = false
		crawlerStatus.Message = fmt.Sprintf("Краулинг завершён. Проиндексировано: %d страниц.", crawlerStatus.PagesFound)
		crawlerStatus.Unlock()
	}()

	crawlerCfg := crawler.Config{
		MaxPages:     cfg.MaxPages,
		MaxDepth:     cfg.MaxDepth,
		Delay:        time.Duration(cfg.DelayMs) * time.Millisecond,
		UserAgent:    cfg.UserAgent,
		AllowedHosts: cfg.AllowedHosts,
	}

	if reqMaxPages > 0 {
		crawlerCfg.MaxPages = reqMaxPages
	}

	c := crawler.New(crawlerCfg, searchEngine)

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

	log.Printf("Crawl started from %d URLs", len(seedURLs))
	c.Crawl(seedURLs)
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

func customFileServer(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean(r.URL.Path))

		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			if r.URL.Path == "/" {
				r.URL.Path = "/index.html"
				fs.ServeHTTP(w, r)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			http.ServeFile(w, r, filepath.Join(dir, "404.html"))
			return
		}

		fs.ServeHTTP(w, r)
	})
}
