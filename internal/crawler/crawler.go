package crawler

import (
	"crypto/md5"
	"encoding/hex"
	"log"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"search_engine/internal/engine"

	"github.com/PuerkitoBio/goquery"
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
		MaxPages:     50,
		MaxDepth:     2,
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
		Parallelism: 2,
		Delay:       cfg.Delay,
	})

	c.collector.OnHTML("html", func(e *colly.HTMLElement) {
		c.mu.Lock()
		defer c.mu.Unlock()

		if c.pageCount >= c.maxPages {
			return
		}

		pageURL := e.Request.URL.String()

		// Проверяем, не проиндексирована ли уже эта страница
		if c.searchEngine.IsURLIndexed(pageURL) {
			log.Printf("URL already indexed, skipping: %s", pageURL)
			return
		}

		// Фильтрация служебных страниц (Википедия и похожие)
		if isServicePage(pageURL) {
			return
		}

		title := e.ChildText("title")
		if title == "" {
			title = e.ChildText("h1")
		}

		content := extractMainContent(e)
		if len(content) < 200 {
			return
		}

		hash := calculateHash(content)

		page := Page{
			URL:         pageURL,
			Title:       cleanTitle(title),
			Content:     content,
			ContentHash: hash,
		}

		c.pages = append(c.pages, page)
		c.pageCount++

		if c.onPage != nil {
			c.onPage(page)
		}

		log.Printf("Crawled [%d/%d]: %s", c.pageCount, c.maxPages, title)
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
			e.Request.Visit(absURL)
		}
	})

	c.collector.OnRequest(func(r *colly.Request) {
		c.mu.Lock()
		if c.pageCount >= c.maxPages {
			r.Abort()
		}
		c.mu.Unlock()
	})

	c.collector.OnError(func(r *colly.Response, err error) {
		log.Printf("Crawl error: %s - %v", r.Request.URL, err)
	})

	return c
}

func (c *Crawler) OnPage(fn func(Page)) {
	c.onPage = fn
}

func (c *Crawler) Crawl(seedURLs []string) []Page {
	done := make(chan bool)

	go func() {
		for _, u := range seedURLs {
			c.collector.Visit(u)
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
			c.collector.Visit(u)
		}
		c.collector.Wait()
	}()
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
	// Декодируем URL, чтобы корректно обрабатывать кириллицу и спецсимволы
	decoded, err := url.QueryUnescape(u)
	if err != nil {
		decoded = u // Если ошибка, используем оригинал
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

func extractMainContent(e *colly.HTMLElement) string {
	// Удаляем весь мусор
	e.DOM.Find("script, style, nav, header, footer, aside, .sidebar, .menu, .navigation, .ads, .advertisement, .cookie, .popup, table.infobox, .reference, .mw-editsection, .navbox").Remove()

	var sb strings.Builder

	// Ищем текст ТОЛЬКО в параграфах
	e.DOM.Find("p").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if len(text) > 0 {
			sb.WriteString(text)
			sb.WriteString(" ")
		}
	})

	content := sb.String()

	if len(content) < 200 {
		content = e.ChildText("body")
	}

	content = cleanText(content)
	return content
}

func cleanText(text string) string {
	patterns := []string{
		`\[\s*(править|правка|edit)\s*\|?\s*(код|source)?\s*\]`,
		`\[\s*\d+\s*\]`,
		`Материал из Википедии.*?энциклопедии`,
		`Перейти к навигации`,
		`Перейти к поиску`,
	}

	for _, p := range patterns {
		re := regexp.MustCompile(`(?i)` + p)
		text = re.ReplaceAllString(text, " ")
	}

	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	return text
}

func cleanTitle(title string) string {
	title = strings.TrimSpace(title)
	title = regexp.MustCompile(`\s*[-|—]\s*.*$`).ReplaceAllString(title, "")
	return title
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
