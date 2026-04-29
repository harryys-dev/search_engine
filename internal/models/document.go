package models

type Document struct {
	ID          int    `json:"id"`
	URL         string `json:"url,omitempty"`
	Title       string `json:"title"`
	Content     string `json:"-"`
	FilePath    string `json:"filePath,omitempty"`
	FileType    string `json:"fileType,omitempty"`
	ContentHash string `json:"-"`
}

type SearchResult struct {
	Document
	Score     float64 `json:"score"`
	Snippet   string  `json:"snippet,omitempty"`
	Relevance string  `json:"relevance"`
}

type SearchResponse struct {
	Results      []SearchResult `json:"results"`
	Total        uint64         `json:"total"`
	Page         int            `json:"page"`
	PageSize     int            `json:"pageSize"`
	TotalPages   int            `json:"totalPages"`
	SearchTime   float64        `json:"searchTime"`
	BleveTime    float64        `json:"bleveTime"`
	Suggestion   string         `json:"suggestion,omitempty"`
	IndexedPages uint64         `json:"indexedPages"`
}

type StatsResponse struct {
	IndexedPages uint64 `json:"indexedPages"`
}
