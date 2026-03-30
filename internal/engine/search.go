package engine

import (
	"errors"
	"fmt"
	"log"
	"search_engine/internal/models"
	"strings"
	"sync"

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

	// Пытаемся открыть существующий индекс
	index, err = bleve.Open(indexPath)
	if errors.Is(err, bleve.ErrorIndexPathDoesNotExist) {
		log.Println("Creating new index...")
		// Если индекса нет, создаем новый
		indexMapping := buildIndexMapping()
		index, err = bleve.New(indexPath, indexMapping)
		if err != nil {
			log.Fatalf("Critical error creating new index: %v", err)
		}
	} else if err != nil {
		// Другая ошибка при открытии
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
	textFieldMapping.IncludeTermVectors = true // Нужно для подсветки

	docMapping.AddFieldMappingsAt("title", textFieldMapping)
	docMapping.AddFieldMappingsAt("content", textFieldMapping)

	// URL должен быть индексируемым для точного поиска, но не для полнотекстового
	keywordFieldMapping := bleve.NewKeywordFieldMapping()
	keywordFieldMapping.Store = true
	docMapping.AddFieldMappingsAt("url", keywordFieldMapping)

	// Поля без индексации, просто для хранения
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
	// Веб-страницы используем URL как ID для уникальности.
	// Для файлов и текстов используем сгенерированный ID.
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

// IsURLIndexed проверяет, был ли уже проиндексирован URL.
func (e *SearchEngine) IsURLIndexed(url string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	doc, err := e.index.Document(url)
	// Если документ найден (doc != nil) и ошибки нет, значит URL уже в индексе
	return doc != nil && err == nil
}

func (e *SearchEngine) Search(queryStr string) []models.SearchResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if queryStr == "" {
		return []models.SearchResult{}
	}

	// Создаем MatchQuery и явно указываем анализатор
	mq := bleve.NewMatchQuery(queryStr)
	mq.Analyzer = "ru"
	mq.Fuzziness = 1 // Опционально: разрешаем небольшие опечатки

	searchRequest := bleve.NewSearchRequest(mq)
	searchRequest.Size = 40              // Берем больше, чтобы потом отфильтровать дубли
	searchRequest.Fields = []string{"*"} // Запрашиваем все сохраненные поля
	searchRequest.IncludeLocations = true

	searchResult, err := e.index.Search(searchRequest)
	if err != nil {
		log.Printf("Search error: %v", err)
		return []models.SearchResult{}
	}

	results := make([]models.SearchResult, 0)
	seenTitles := make(map[string]bool)

	for _, hit := range searchResult.Hits {
		// Восстанавливаем документ из полей, сохраненных в Bleve
		doc := models.Document{
			ID:       0, // ID не хранится в полях, но он нам и не нужен для результата
			URL:      safeStringFromField(hit.Fields["url"]),
			Title:    safeStringFromField(hit.Fields["title"]),
			Content:  safeStringFromField(hit.Fields["content"]),
			FilePath: safeStringFromField(hit.Fields["filePath"]),
			FileType: safeStringFromField(hit.Fields["fileType"]),
		}
		if hash, ok := hit.Fields["contentHash"].(string); ok {
			doc.ContentHash = hash
		}

		// Дополнительная фильтрация дублей по заголовку (иногда контент чуть разный, а заголовок тот же)
		cleanTitle := strings.ToLower(strings.TrimSpace(doc.Title))
		if seenTitles[cleanTitle] {
			continue
		}
		seenTitles[cleanTitle] = true

		if len(results) >= 20 {
			break
		}

		// Генерируем красивый идеальный сниппет, используя данные о совпадениях от Bleve
		snippet := GenerateSnippetWithLocations(doc.Content, queryStr, hit.Locations["content"])

		results = append(results, models.SearchResult{
			Document:  doc,
			Score:     hit.Score,
			Snippet:   snippet,
			Relevance: calculateRelevance(hit.Score, searchResult.MaxScore),
		})
	}

	return results
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
