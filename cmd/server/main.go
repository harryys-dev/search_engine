package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path"
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

	// Ensure built assets contain fallback 404 and expected gopher image so custom 404 and empty-state icons work
	distDir := "./web/dist"
	// copy 404.html to dist if missing
	if _, err := os.Stat(filepath.Join(distDir, "404.html")); os.IsNotExist(err) {
		src404 := "./web/404.html"
		if _, err := os.Stat(src404); err == nil {
			if err := copyFile(src404, filepath.Join(distDir, "404.html")); err != nil {
				log.Printf("Failed to copy 404.html to dist: %v", err)
			}
		}
	}
	// ensure a gopher image exists under dist/assets with a stable name
	gopherTarget := filepath.Join(distDir, "assets", "gopher-cold-sweat.png")
	if _, err := os.Stat(gopherTarget); os.IsNotExist(err) {
		assetsDir := filepath.Join(distDir, "assets")
		if entries, err := os.ReadDir(assetsDir); err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "gopher") && strings.HasSuffix(e.Name(), ".png") {
					src := filepath.Join(assetsDir, e.Name())
					if err := copyFile(src, gopherTarget); err != nil {
						log.Printf("Failed to copy gopher image: %v", err)
					}
					break
				}
			}
		}
	}

	crawlerCfg := loadCrawlerConfig()

	if crawlerCfg.AutoStart && len(crawlerCfg.SeedURLs) > 0 {
		go autoStartCrawler(crawlerCfg)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/search", handleSearch)
	mux.HandleFunc("/stats", handleStats)
	mux.Handle("/upload", requireAdminAccess(http.HandlerFunc(handleUpload)))
	mux.Handle("/crawl", requireAdminAccess(http.HandlerFunc(handleCrawl)))
	mux.Handle("/crawl/status", requireAdminAccess(http.HandlerFunc(handleCrawlStatus)))
	mux.Handle("/files/", uploadedFileServer("./uploads"))
	mux.Handle("/", customFileServer("./web/dist"))

	server := &http.Server{
		Addr:         getListenAddr(),
		Handler:      securityHeadersMiddleware(corsMiddleware(mux)),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	fmt.Printf("Сервер запущен на http://%s\n", server.Addr)
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
	allowed := allowedOrigins()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			} else if r.Method == http.MethodOptions {
				http.Error(w, "Origin not allowed", http.StatusForbidden)
				return
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' http://localhost:8080 http://127.0.0.1:8080; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", filePath, "-")
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("pdftotext timed out")
		}
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

	const maxUploadSize = 20 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

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
	if !isAllowedUploadExtension(ext) {
		http.Error(w, "Unsupported file type", http.StatusBadRequest)
		return
	}

	originalName := filepath.Base(handler.Filename)
	titleWithoutExt := sanitizeDisplayText(strings.TrimSuffix(originalName, ext))

	fileBytes, err := io.ReadAll(io.LimitReader(file, maxUploadSize+1))
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}
	if len(fileBytes) == 0 || len(fileBytes) > maxUploadSize {
		http.Error(w, "Invalid file size", http.StatusBadRequest)
		return
	}
	if err := validateUploadBytes(ext, fileBytes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	storedFilename, err := randomFilename(ext)
	if err != nil {
		http.Error(w, "Failed to prepare file storage", http.StatusInternalServerError)
		return
	}
	dstPath := filepath.Join("uploads", storedFilename)

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
		FilePath: "/files/" + storedFilename,
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
		safeURL, err := crawler.NormalizeSeedURL(req.URL, cfg.AllowedHosts)
		if err != nil {
			http.Error(w, "Unsafe crawl URL", http.StatusBadRequest)
			return
		}
		seedURLs = append([]string{safeURL}, seedURLs...)
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

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func customFileServer(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := path.Clean("/" + r.URL.Path)
		if cleanPath == "/" {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}

		relativePath := strings.TrimPrefix(cleanPath, "/")
		localPath := filepath.Join(dir, filepath.FromSlash(relativePath))

		info, err := os.Stat(localPath)
		if err != nil || info.IsDir() {
			// If dist doesn't contain a 404.html, try to serve fallback from source folder
			four04 := filepath.Join(dir, "404.html")
			if _, err := os.Stat(four04); os.IsNotExist(err) {
				fallback := filepath.Join(filepath.Dir(dir), "404.html")
				if _, err := os.Stat(fallback); err == nil {
					http.ServeFile(w, r, fallback)
					return
				}
			}

			http.ServeFile(w, r, four04)
			return
		}

		fs.ServeHTTP(w, r)
	})
}

func uploadedFileServer(root string) http.Handler {
	return http.StripPrefix("/files/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Base("/" + r.URL.Path)
		if name == "." || name == "/" {
			http.NotFound(w, r)
			return
		}

		fullPath := filepath.Join(root, name)
		if _, err := os.Stat(fullPath); err != nil {
			http.NotFound(w, r)
			return
		}

		ext := strings.ToLower(filepath.Ext(name))
		if shouldForceDownload(ext) {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
			w.Header().Set("Content-Type", "application/octet-stream")
		} else if contentType := mime.TypeByExtension(ext); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}

		http.ServeFile(w, r, fullPath)
	}))
}

func shouldForceDownload(ext string) bool {
	switch ext {
	case ".html", ".htm", ".svg", ".xml":
		return true
	default:
		return false
	}
}

func isAllowedUploadExtension(ext string) bool {
	switch ext {
	case ".pdf", ".html", ".htm":
		return true
	default:
		return false
	}
}

func validateUploadBytes(ext string, content []byte) error {
	detected := http.DetectContentType(content)
	switch ext {
	case ".pdf":
		if !bytes.HasPrefix(content, []byte("%PDF-")) || detected != "application/pdf" {
			return fmt.Errorf("invalid PDF file")
		}
	case ".html", ".htm":
		lower := strings.ToLower(string(content))
		if !(strings.Contains(lower, "<html") || strings.Contains(lower, "<body") || strings.Contains(lower, "<p")) {
			return fmt.Errorf("invalid HTML file")
		}
	default:
		return fmt.Errorf("unsupported file type")
	}

	return nil
}

func randomFilename(ext string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf) + ext, nil
}

func sanitizeDisplayText(value string) string {
	value = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case r < 32:
			return -1
		default:
			return r
		}
	}, value)

	value = strings.TrimSpace(value)
	if value == "" {
		return "document"
	}
	return value
}
