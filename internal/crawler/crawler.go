package crawler

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"search_engine/internal/engine"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/gocolly/colly/v2"
)

type Page struct {
	URL         string
	Title       string
	Content     string
	ContentHash string
}

type Crawler struct {
	collector    *colly.Collector
	pages        []Page
	mu           sync.Mutex
	maxPages     int
	pageCount    int
	onPage       func(Page)
	allowedHosts []string
	searchEngine *engine.SearchEngine // Добавляем экземпляр поискового движка
}

type Config struct {
	MaxPages     int
	MaxDepth     int
	AllowedHosts []string
	Delay        time.Duration
	UserAgent    string
}

func DefaultConfig() Config {
	return Config{
		MaxPages:     5000,
		MaxDepth:     4,
		AllowedHosts: []string{},
		Delay:        500 * time.Millisecond,
		UserAgent:    "GoSearchEngine/1.0",
	}
}

func New(cfg Config, searchEngine *engine.SearchEngine) *Crawler {
	c := &Crawler{
		pages:        make([]Page, 0),
		maxPages:     cfg.MaxPages,
		allowedHosts: cfg.AllowedHosts,
		searchEngine: searchEngine, // Сохраняем экземпляр
	}

	opts := []colly.CollectorOption{
		colly.MaxDepth(cfg.MaxDepth),
		colly.Async(true),
		colly.UserAgent(cfg.UserAgent),
	}

	if len(cfg.AllowedHosts) > 0 {
		opts = append(opts, colly.AllowedDomains(cfg.AllowedHosts...))
	}

	c.collector = colly.NewCollector(opts...)

	c.collector.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 5,
		Delay:       cfg.Delay,
	})

	c.collector.OnXML("//sitemap/loc|//urlset/url/loc", func(e *colly.XMLElement) {
		target := e.Text
		if isValidURL(target) && !isMediaURL(target) {
			c.safeVisit(target)
		}
	})

	c.collector.OnHTML("html", func(e *colly.HTMLElement) {
		c.mu.Lock()
		if c.pageCount >= c.maxPages {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		pageURL := e.Request.URL

		if c.searchEngine.IsURLIndexed(pageURL.String()) {
			log.Printf("URL already indexed, skipping: %s", pageURL)
			return
		}

		if isServicePage(pageURL.String()) {
			return
		}

		article, err := readability.FromDocument(e.DOM.Get(0).Parent, pageURL)
		if err != nil {
			log.Printf("Readability error for %s: %v", pageURL, err)
			return
		}

		var textBuf strings.Builder
		article.RenderText(&textBuf)
		content := textBuf.String()
		content = stripBoilerplate(content) // ← сюда
		content = stripCombining(content)
		if len(content) < 200 {
			return
		}

		hash := calculateHash(content)

		page := Page{
			URL:         pageURL.String(),
			Title:       article.Title(),
			Content:     content,
			ContentHash: hash,
		}

		c.mu.Lock()
		c.pages = append(c.pages, page)
		c.pageCount++
		c.mu.Unlock()

		if c.onPage != nil {
			c.onPage(page)
		}

		log.Printf("Crawled [%d/%d]: %s", c.pageCount, c.maxPages, article.Title())
	})

	c.collector.OnHTML("a[href]", func(e *colly.HTMLElement) {
		c.mu.Lock()
		if c.pageCount >= c.maxPages {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()

		link := e.Attr("href")
		absURL := e.Request.AbsoluteURL(link)

		if isValidURL(absURL) && !isMediaURL(absURL) {
			if _, err := validateCrawlURL(absURL, c.allowedHosts); err != nil {
				log.Printf("Skipping unsafe link %q: %v", absURL, err)
				return
			}
			e.Request.Visit(absURL)
		}
	})

	c.collector.OnRequest(func(r *colly.Request) {
		c.mu.Lock()
		if c.pageCount >= c.maxPages {
			r.Abort()
		}
		c.mu.Unlock()

		if _, err := validateCrawlURL(r.URL.String(), c.allowedHosts); err != nil {
			log.Printf("Blocking unsafe request %q: %v", r.URL.String(), err)
			r.Abort()
		}
	})

	c.collector.OnError(func(r *colly.Response, err error) {
		log.Printf("Crawl error: %s - %v", r.Request.URL, err)
	})

	return c
}

func stripCombining(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stripBoilerplate(text string) string {
	prefixes := []string{
		"Материал из Википедии",
		"Материал из Вики",
		"Медиафайлы на Викискладе",
		"From Wikipedia",
		"Jump to navigation",
		"Перейти к навигации",
		"Перейти к поиску",
	}

	cleaned := text
	for _, prefix := range prefixes {
		if idx := strings.Index(cleaned, prefix); idx >= 0 && idx < 100 {
			end := strings.Index(cleaned[idx:], "\n")
			if end == -1 {
				end = strings.Index(cleaned[idx:], ". ")
				if end == -1 {
					end = len(prefix)
				}
			}
			cleaned = cleaned[idx+end:]
			cleaned = strings.TrimSpace(cleaned)
		}
	}

	for strings.HasPrefix(cleaned, "\n") {
		cleaned = strings.TrimPrefix(cleaned, "\n")
	}
	cleaned = strings.TrimSpace(cleaned)

	return cleaned
}

func (c *Crawler) OnPage(fn func(Page)) {
	c.onPage = fn
}

func (c *Crawler) Crawl(seedURLs []string) []Page {
	sitemapURLs := make([]string, 0, len(seedURLs))
	for _, u := range seedURLs {
		safeURL, err := NormalizeSeedURL(u, c.allowedHosts)
		if err != nil {
			log.Printf("Skipping unsafe seed URL %q: %v", u, err)
			continue
		}

		parsed, _ := url.Parse(safeURL)
		sitemapURLs = append(sitemapURLs, parsed.Scheme+"://"+parsed.Host+"/sitemap.xml")
	}

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			_, err := validateCrawlURL(req.URL.String(), c.allowedHosts)
			return err
		},
	}

	for _, su := range sitemapURLs {
		if _, err := validateCrawlURL(su, c.allowedHosts); err != nil {
			log.Printf("Skipping unsafe sitemap URL %q: %v", su, err)
			continue
		}

		resp, err := httpClient.Get(su)
		if err == nil {
			resp.Body.Close()
			log.Printf("Found sitemap: %s", su)
			c.safeVisit(su)
		}
	}

	done := make(chan bool)

	go func() {
		for _, u := range seedURLs {
			c.safeVisit(u)
		}
		c.collector.Wait()
		done <- true
	}()

	timeout := time.After(2 * time.Minute)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return c.pages
		case <-timeout:
			log.Println("Crawler timeout reached")
			return c.pages
		case <-ticker.C:
			c.mu.Lock()
			if c.pageCount >= c.maxPages {
				c.mu.Unlock()
				time.Sleep(2 * time.Second)
				return c.pages
			}
			c.mu.Unlock()
		}
	}
}

func (c *Crawler) CrawlAsync(seedURLs []string) {
	go func() {
		for _, u := range seedURLs {
			c.safeVisit(u)
		}
		c.collector.Wait()
	}()
}

func (c *Crawler) safeVisit(rawURL string) {
	safeURL, err := NormalizeSeedURL(rawURL, c.allowedHosts)
	if err != nil {
		log.Printf("Skipping unsafe crawl target %q: %v", rawURL, err)
		return
	}

	if err := c.collector.Visit(safeURL); err != nil {
		log.Printf("Visit skipped for %q: %v", safeURL, err)
	}
}

func (c *Crawler) GetPages() []Page {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pages
}

func (c *Crawler) PageCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pageCount
}

func isServicePage(u string) bool {
	decoded, err := url.QueryUnescape(u)
	if err != nil {
		decoded = u
	}

	servicePatterns := []string{
		`action=`,
		`oldid=`,
		`diff=`,
		`title=Special:`,
		`title=Служебная:`,
		`Special:Search`,
		`Special:RecentChanges`,
		`_(значения)`,
		`_(значение)`,
		"мои-комментарии",
		"добавить-новую",
		"сообщить-об-ошибке",
		"шпаргалка",
		"правообладателям",
		"/dmca",
		"/license",
		"пользовательское-соглашение",
		"политика-конфиденциальности",
		"github.com",
		"opensource.org",
	}

	for _, p := range servicePatterns {
		if strings.Contains(decoded, p) {
			return true
		}
	}
	return false
}

func calculateHash(text string) string {
	hash := md5.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}

func isValidURL(u string) bool {
	if u == "" {
		return false
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func isMediaURL(u string) bool {
	mediaExts := []string{".jpg", ".jpeg", ".png", ".gif", ".svg", ".webp", ".mp4", ".mp3", ".pdf", ".zip", ".rar", ".exe", ".css", ".js"}
	lower := strings.ToLower(u)
	for _, ext := range mediaExts {
		if strings.HasSuffix(lower, ext) || strings.Contains(lower, ext+"?") {
			return true
		}
	}
	return false
}
