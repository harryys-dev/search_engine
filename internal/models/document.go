package models

type Document struct {
	ID          int    `json:"id"`
	URL         string `json:"url,omitempty"` // Добавляем URL
	Title       string `json:"title"`
	Content     string `json:"content"`
	FilePath    string `json:"filePath,omitempty"`
	FileType    string `json:"fileType,omitempty"`
	ContentHash string `json:"contentHash,omitempty"`
}

type SearchResult struct {
	Document
	Score     float64 `json:"score"`
	Snippet   string  `json:"snippet,omitempty"`
	Relevance string  `json:"relevance"`
}
