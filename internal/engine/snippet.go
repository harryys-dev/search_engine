package engine

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2/search"
)

type ByteRange struct {
	Start int
	End   int
}

//go:embed data/stopwords-ru.json
var stopwordsRU []byte

//go:embed data/stopwords-en.json
var stopwordsEN []byte

var stopWords map[string]bool

func init() {
	stopWords = make(map[string]bool)
	loadStopwords(stopwordsRU)
	loadStopwords(stopwordsEN)
}

func loadStopwords(data []byte) {
	var words []string
	if err := json.Unmarshal(data, &words); err != nil {
		return
	}
	for _, w := range words {
		stopWords[strings.ToLower(w)] = true
	}
}

func isStopWord(text string, start, end int) bool {
	word := strings.ToLower(strings.TrimSpace(text[start:end]))
	return stopWords[word]
}

// GenerateSnippetWithLocations создает стильный сниппет, используя точные байтовые границы совпадений от Bleve
func GenerateSnippetWithLocations(text string, query string, termLocs search.TermLocationMap) string {
	if text == "" {
		return ""
	}

	if len(termLocs) == 0 {
		return truncateString(text, 250)
	}

	// Собираем все интервалы совпадений
	var ranges []ByteRange
	for _, locs := range termLocs {
		for _, loc := range locs {
			ranges = append(ranges, ByteRange{Start: int(loc.Start), End: int(loc.End)})
		}
	}

	if len(ranges) == 0 {
		return truncateString(text, 250)
	}

	// Сортируем интервалы по началу
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	// Пробуем найти совпадение ближе к началу текста,
	// если первое от Bleve далеко
	// Пробуем найти совпадение ближе к началу текста,
	// если первое от Bleve далеко
	// Сортируем интервалы по началу
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	// ВСЕГДА ищем слова запроса в первых 500 байтах
	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	filtered := make([]string, 0, len(queryWords))
	for _, w := range queryWords {
		if !stopWords[w] && len(w) > 1 {
			filtered = append(filtered, w)
		}
	}

	scanLimit := 500
	if scanLimit > len(text) {
		scanLimit = len(text)
	}

	for _, word := range filtered {
		byteStart, byteEnd, found := findWordNormalized(text[:scanLimit], word)
		if !found {
			continue
		}

		duplicate := false
		for _, existing := range ranges {
			if existing.Start == byteStart && existing.End == byteEnd {
				duplicate = true
				break
			}
		}
		if !duplicate {
			ranges = append([]ByteRange{{Start: byteStart, End: byteEnd}}, ranges...)
		}
		break
	}

	// Сортируем ещё раз после вставок
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	// Теперь ranges[0] — ближайшее к началу совпадение
	bestIdx := ranges[0].Start

	// Ищем начало предложения (назад до 150 байт)
	start := bestIdx
	maxLookback := 150
	limit := 0
	if start > maxLookback {
		limit = start - maxLookback
	}

	for start > limit {
		if start > 0 {
			if !utf8.RuneStart(text[start-1]) {
				start--
				continue
			}
			ch := text[start-1]
			if ch == '.' || ch == '!' || ch == '?' || ch == '\n' {
				break
			}
		}
		start--
	}

	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}

	for start < len(text) && (text[start] == ' ' || text[start] == '\t' || text[start] == '\n' || text[start] == '\r') {
		start++
	}

	// Отмеряем 250 байт от начала сниппета для конца
	end := start + 250
	if end >= len(text) {
		end = len(text)
	} else {
		for end > start && !utf8.RuneStart(text[end]) {
			end--
		}
		spaceIdx := strings.LastIndex(text[start:end], " ")
		if spaceIdx > 120 {
			end = start + spaceIdx
		}
	}

	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}

	// Строим сниппет
	var builder strings.Builder

	hasContentBefore := false
	for i := 0; i < start; i++ {
		ch := text[i]
		if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' {
			hasContentBefore = true
			break
		}
	}
	if hasContentBefore {
		builder.WriteString("...")
	}

	currentByte := start
	for _, r := range ranges {
		if r.End <= start {
			continue
		}
		if r.Start >= end {
			break
		}

		mStart := r.Start
		if mStart < start {
			mStart = start
		}
		mEnd := r.End
		if mEnd > end {
			mEnd = end
		}

		if isStopWord(text, mStart, mEnd) {
			continue
		}

		if mStart > currentByte {
			builder.WriteString(escapeHTML(text[currentByte:mStart]))
		}

		if mStart >= currentByte {
			builder.WriteString("<mark>")
			builder.WriteString(escapeHTML(text[mStart:mEnd]))
			builder.WriteString("</mark>")
			currentByte = mEnd
		}
	}

	if currentByte < end {
		builder.WriteString(escapeHTML(text[currentByte:end]))
	}

	if end < len(text) {
		builder.WriteString("...")
	}

	return builder.String()
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

// findWordNormalized ищет слово в тексте, игнорируя combining marks.
// Возвращает байтовые позиции в ОРИГИНАЛЬНОМ тексте.
func findWordNormalized(text, word string) (int, int, bool) {
	cleanText := stripCombining(strings.ToLower(text))
	cleanWord := stripCombining(strings.ToLower(word))

	idx := strings.Index(cleanText, cleanWord)
	if idx < 0 {
		return 0, 0, false
	}

	// Мапим позицию руны из cleaned обратно в байты оригинала
	origRunes := []rune(text)
	origIdx := 0
	cleanIdx := 0
	for origIdx < len(origRunes) && cleanIdx < idx {
		if !unicode.Is(unicode.Mn, origRunes[origIdx]) {
			cleanIdx++
		}
		origIdx++
	}

	// Байтовый старт
	byteStart := 0
	for i := 0; i < origIdx; i++ {
		byteStart += utf8.RuneLen(origRunes[i])
	}

	// Байтовый конец — пропускаем wordLen non-combining рун
	wordLen := utf8.RuneCountInString(cleanWord)
	byteEnd := byteStart
	endOrigIdx := origIdx
	for wordLen > 0 && endOrigIdx < len(origRunes) {
		byteEnd += utf8.RuneLen(origRunes[endOrigIdx])
		if !unicode.Is(unicode.Mn, origRunes[endOrigIdx]) {
			wordLen--
		}
		endOrigIdx++
	}

	return byteStart, byteEnd, true
}

func truncateString(str string, length int) string {
	runes := []rune(str)
	if len(runes) > length {
		return escapeHTML(string(runes[:length])) + "..."
	}
	return escapeHTML(str)
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
