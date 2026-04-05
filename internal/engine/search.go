package engine

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	uni "unicode"
	"unicode/utf8"

	"search_engine/internal/models"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	"github.com/blevesearch/bleve/v2/analysis/lang/ru"
	"github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
	"github.com/blevesearch/bleve/v2/mapping"
)

type BleveDocument struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	FilePath    string `json:"filePath"`
	FileType    string `json:"fileType"`
	ContentHash string `json:"contentHash"`
}

type SearchEngine struct {
	mu        sync.RWMutex
	index     bleve.Index
	indexPath string
}

func NewEngine() *SearchEngine {
	indexPath := "search.bleve"
	var index bleve.Index
	var err error

	index, err = bleve.Open(indexPath)
	if errors.Is(err, bleve.ErrorIndexPathDoesNotExist) {
		log.Println("Creating new index...")
		indexMapping := buildIndexMapping()
		index, err = bleve.New(indexPath, indexMapping)
		if err != nil {
			log.Fatalf("Critical error creating new index: %v", err)
		}
	} else if err != nil {
		log.Fatalf("Critical error opening index: %v", err)
	} else {
		log.Println("Opened existing index.")
	}

	return &SearchEngine{
		index:     index,
		indexPath: indexPath,
	}
}

func buildIndexMapping() mapping.IndexMapping {
	ruAnalyzer := map[string]interface{}{
		"type":      custom.Name,
		"tokenizer": unicode.Name,
		"token_filters": []string{
			lowercase.Name,
			ru.StopName,
			ru.SnowballStemmerName,
		},
	}

	indexMapping := bleve.NewIndexMapping()
	err := indexMapping.AddCustomAnalyzer("ru", ruAnalyzer)
	if err != nil {
		log.Printf("Error adding analyzer: %v", err)
	}
	indexMapping.DefaultAnalyzer = "ru"

	docMapping := bleve.NewDocumentMapping()

	textFieldMapping := bleve.NewTextFieldMapping()
	textFieldMapping.Analyzer = "ru"
	textFieldMapping.Store = true
	textFieldMapping.IncludeTermVectors = true

	docMapping.AddFieldMappingsAt("title", textFieldMapping)
	docMapping.AddFieldMappingsAt("content", textFieldMapping)

	keywordFieldMapping := bleve.NewKeywordFieldMapping()
	keywordFieldMapping.Store = true
	docMapping.AddFieldMappingsAt("url", keywordFieldMapping)

	storeFieldMapping := bleve.NewTextFieldMapping()
	storeFieldMapping.Store = true
	storeFieldMapping.Index = false
	docMapping.AddFieldMappingsAt("filePath", storeFieldMapping)
	docMapping.AddFieldMappingsAt("fileType", storeFieldMapping)
	docMapping.AddFieldMappingsAt("contentHash", storeFieldMapping)

	indexMapping.AddDocumentMapping("document", docMapping)
	return indexMapping
}

func (e *SearchEngine) Index(doc models.Document) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var docID string
	if doc.URL != "" {
		docID = doc.URL
	} else {
		docID = fmt.Sprintf("doc_%d", doc.ID)
	}

	bleveDoc := BleveDocument{
		ID:          docID,
		URL:         doc.URL,
		Title:       doc.Title,
		Content:     doc.Content,
		FilePath:    doc.FilePath,
		FileType:    doc.FileType,
		ContentHash: doc.ContentHash,
	}

	if err := e.index.Index(docID, bleveDoc); err != nil {
		log.Printf("Index error for doc %s: %v", docID, err)
	}
}

func (e *SearchEngine) IsURLIndexed(url string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	doc, err := e.index.Document(url)
	return doc != nil && err == nil
}

func (e *SearchEngine) DocCount() uint64 {
	count, err := e.index.DocCount()
	if err != nil {
		return 0
	}
	return count
}

func (e *SearchEngine) SearchPaginated(queryStr string, page, size int) models.SearchResponse {
	e.mu.RLock()
	defer e.mu.RUnlock()

	bleveStart := time.Now()
	fullStart := time.Now()

	indexedPages := e.DocCount()

	emptyResponse := func(suggestion string) models.SearchResponse {
		return models.SearchResponse{
			Results:      []models.SearchResult{},
			Total:        0,
			Page:         page,
			PageSize:     size,
			TotalPages:   0,
			SearchTime:   time.Since(fullStart).Seconds(),
			BleveTime:    time.Since(bleveStart).Seconds(), // чистое время Bleve
			Suggestion:   suggestion,
			IndexedPages: indexedPages,
		}
	}

	if queryStr == "" {
		return emptyResponse("")
	}

	cleanedQuery := filterStopWords(queryStr)
	if cleanedQuery == "" {
		cleanedQuery = queryStr
	}

	from := (page - 1) * size
	contentQuery := bleve.NewMatchQuery(cleanedQuery)
	contentQuery.Analyzer = "ru"
	contentQuery.SetField("content")

	titleQuery := bleve.NewMatchQuery(cleanedQuery)
	titleQuery.Analyzer = "ru"
	titleQuery.SetField("title")

	bq := bleve.NewBooleanQuery()
	bq.AddShould(contentQuery)
	bq.AddShould(titleQuery)

	searchRequest := bleve.NewSearchRequest(bq)
	searchRequest.Fields = []string{"*"}
	searchRequest.IncludeLocations = true

	searchRequest.From = from
	searchRequest.Size = size

	searchResult, err := e.index.Search(searchRequest)
	bleveTime := time.Since(bleveStart)

	if err != nil {
		log.Printf("Search error: %v", err)
		return emptyResponse("")
	}

	totalResults := searchResult.Total
	totalPages := int(totalResults) / size
	if int(totalResults)%size > 0 {
		totalPages++
	}

	results := make([]models.SearchResult, 0, len(searchResult.Hits))
	seenTitles := make(map[string]bool)

	for _, hit := range searchResult.Hits {
		doc := models.Document{
			URL:      safeStringFromField(hit.Fields["url"]),
			Title:    safeStringFromField(hit.Fields["title"]),
			Content:  safeStringFromField(hit.Fields["content"]),
			FilePath: safeStringFromField(hit.Fields["filePath"]),
			FileType: safeStringFromField(hit.Fields["fileType"]),
		}
		if hash, ok := hit.Fields["contentHash"].(string); ok {
			doc.ContentHash = hash
		}

		cleanTitle := strings.ToLower(strings.TrimSpace(doc.Title))
		if seenTitles[cleanTitle] {
			continue
		}
		seenTitles[cleanTitle] = true

		snippet := GenerateSnippetWithLocations(doc.Content, cleanedQuery, hit.Locations["content"])

		results = append(results, models.SearchResult{
			Document:  doc,
			Score:     hit.Score,
			Snippet:   snippet,
			Relevance: calculateRelevance(hit.Score, searchResult.MaxScore),
		})
	}

	suggestion := ""
	if len(results) == 0 {
		suggestion = e.suggest(queryStr)
	}

	return models.SearchResponse{
		Results:      results,
		Total:        totalResults,
		Page:         page,
		PageSize:     size,
		TotalPages:   totalPages,
		SearchTime:   time.Since(fullStart).Seconds(),
		BleveTime:    bleveTime.Seconds(),
		Suggestion:   suggestion,
		IndexedPages: indexedPages,
	}
}

func (e *SearchEngine) suggest(query string) string {
	words := strings.Fields(query)
	meaningfulWords := make([]string, 0)

	for _, w := range words {
		lower := strings.ToLower(w)
		if !stopWords[lower] && utf8.RuneCountInString(lower) > 2 {
			meaningfulWords = append(meaningfulWords, lower)
		}
	}

	if len(meaningfulWords) == 0 {
		return ""
	}

	fuzzyQuery := bleve.NewMatchQuery(strings.Join(meaningfulWords, " "))
	fuzzyQuery.Analyzer = "ru"
	fuzzyQuery.SetFuzziness(2)

	req := bleve.NewSearchRequest(fuzzyQuery)
	req.Size = 3
	req.Fields = []string{"title", "content"}

	result, err := e.index.Search(req)
	if err != nil || result.Total == 0 {
		return ""
	}

	docWords := make(map[string]bool)
	for _, hit := range result.Hits {
		title := strings.ToLower(safeStringFromField(hit.Fields["title"]))
		content := strings.ToLower(safeStringFromField(hit.Fields["content"]))
		for _, w := range strings.Fields(title + " " + content) {
			if utf8.RuneCountInString(w) > 2 && !stopWords[w] {
				docWords[w] = true
			}
		}
	}

	corrections := make(map[string]string)
	for _, qw := range meaningfulWords {
		if docWords[qw] {
			continue
		}

		bestMatch := ""
		bestDist := 999
		for dw := range docWords {
			dist := levenshteinRune(qw, dw)
			if dist < bestDist && dist <= 3 {
				bestDist = dist
				bestMatch = dw
			}
		}

		if bestMatch != "" {
			corrections[qw] = bestMatch
		}
	}

	if len(corrections) == 0 {
		return ""
	}

	suggestion := make([]string, 0, len(words))
	for _, w := range words {
		lower := strings.ToLower(w)
		if corr, ok := corrections[lower]; ok {
			runes := []rune(corr)
			if len(runes) > 0 {
				firstOrig := []rune(w)[0]
				if uni.IsUpper(firstOrig) {
					runes[0] = uni.ToUpper(runes[0])
				}
			}
			suggestion = append(suggestion, string(runes))
		} else {
			suggestion = append(suggestion, w)
		}
	}

	return strings.Join(suggestion, " ")
}

func levenshteinRune(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)
	la := len(ra)
	lb := len(rb)

	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}

	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min3(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
		}
	}

	return d[la][lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func filterStopWords(query string) string {
	words := strings.Fields(query)
	filtered := make([]string, 0, len(words))
	for _, w := range words {
		if !stopWords[strings.ToLower(w)] {
			filtered = append(filtered, w)
		}
	}
	return strings.Join(filtered, " ")
}

func calculateRelevance(score, maxScore float64) string {
	if maxScore == 0 {
		return "low"
	}
	ratio := score / maxScore
	if ratio >= 0.7 {
		return "high"
	} else if ratio >= 0.4 {
		return "medium"
	}
	return "low"
}

func (e *SearchEngine) Close() {
	if e.index != nil {
		_ = e.index.Close()
	}
}

func safeStringFromField(field interface{}) string {
	if field == nil {
		return ""
	}
	s, ok := field.(string)
	if !ok {
		return ""
	}
	return s
}
