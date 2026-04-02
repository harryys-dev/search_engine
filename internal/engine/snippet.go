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

	// ВСЕГДА ищем ВСЕ вхождения слов запроса в первых 500 байтах,
	// чтобы выбрать лучшее (с заглавной буквы, после определения и т.д.)
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

	type candidate struct {
		byteStart int
		byteEnd   int
		score     int
	}

	var best *candidate

	for _, word := range filtered {
		// Ищем ВСЕ вхождения слова
		allMatches := findAllWordNormalized(text[:scanLimit], word)

		for _, m := range allMatches {
			score := 0

			// +100 если слово с заглавной буквы
			firstRune, _ := utf8.DecodeRuneInString(text[m.byteStart:])
			if unicode.IsUpper(firstRune) {
				score += 100
			}

			// +50 если стоит в начале предложения (после . ! ? \n или в начале текста)
			if m.byteStart > 0 {
				preceding := strings.TrimRight(text[:m.byteStart], " \t\n\r")
				if len(preceding) == 0 || preceding[len(preceding)-1] == '.' || preceding[len(preceding)-1] == '!' || preceding[len(preceding)-1] == '?' {
					score += 50
				}
			} else {
				score += 30
			}

			// +200 если после слова идёт ( или — — паттерн определения
			remaining := strings.TrimSpace(text[m.byteEnd:])
			if len(remaining) > 0 {
				nextRune, _ := utf8.DecodeRuneInString(remaining)
				if nextRune == '(' || nextRune == '—' || nextRune == '–' || nextRune == '-' {
					score += 200
				}
			}

			// -150 если после слова идёт мусорная строка (Викискладе и т.д.)
			nextNewline := strings.Index(text[m.byteEnd:], "\n")
			if nextNewline > 0 && nextNewline < 100 {
				nextLine := strings.TrimSpace(text[m.byteEnd : m.byteEnd+nextNewline])
				if isBoilerplateLine(nextLine) {
					score -= 150
				}
			}

			// -100 если ПЕРЕД словом идёт мусорная строка
			prevNewline := -1
			for i := m.byteStart - 1; i >= 0; i-- {
				if text[i] == '\n' {
					prevNewline = i
					break
				}
			}
			if prevNewline >= 0 {
				prevLine := strings.TrimSpace(text[prevNewline:m.byteStart])
				if isBoilerplateLine(prevLine) {
					score -= 100
				}
			}

			if best == nil || score > best.score {
				best = &candidate{byteStart: m.byteStart, byteEnd: m.byteEnd, score: score}
			}
		}

		// Если нашли идеальный кандидат (заглавная + определение) — не ищем дальше
		if best != nil && best.score >= 300 {
			break
		}
	}

	if best != nil {
		duplicate := false
		for _, existing := range ranges {
			if existing.Start == best.byteStart && existing.End == best.byteEnd {
				duplicate = true
				break
			}
		}
		if !duplicate {
			ranges = append([]ByteRange{{Start: best.byteStart, End: best.byteEnd}}, ranges...)
		}
	}

	// Сортируем ещё раз после вставок
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	// Теперь ranges[0] — лучшее совпадение
	// ... цикл поиска best ...
	// ... вставка best в ranges ...
	// ... сортировка ranges ...

	var bestIdx int
	if best != nil {
		bestIdx = best.byteStart
	} else {
		bestIdx = ranges[0].Start
	}

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

func isBoilerplateLine(line string) bool {
	lower := strings.ToLower(line)
	boilerplate := []string{
		"медиафайлы на викискладе",
		"материал из википедии",
		"перейти к навигации",
		"перейти к поиску",
		"см. также",
		"навигация",
	}
	for _, b := range boilerplate {
		if strings.Contains(lower, b) {
			return true
		}
	}
	return false
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

// findAllWordNormalized ищет ВСЕ вхождения слова в тексте, игнорируя combining marks.
// Возвращает байтовые позиции в ОРИГИНАЛЬНОМ тексте.
func findAllWordNormalized(text, word string) []struct {
	byteStart int
	byteEnd   int
} {
	cleanText := stripCombining(strings.ToLower(text))
	cleanWord := stripCombining(strings.ToLower(word))

	var results []struct {
		byteStart int
		byteEnd   int
	}

	origRunes := []rune(text)
	cleanRunes := []rune(cleanText)

	// Ищем все вхождения cleanWord в cleanText
	searchRunes := []rune(cleanWord)
	if len(searchRunes) == 0 {
		return results
	}

	for i := 0; i <= len(cleanRunes)-len(searchRunes); i++ {
		// Проверяем совпадение
		match := true
		for j := 0; j < len(searchRunes); j++ {
			if cleanRunes[i+j] != searchRunes[j] {
				match = false
				break
			}
		}
		if !match {
			continue
		}

		// Мапим позицию cleanRunes[i] обратно в байты оригинала
		byteStart := runeIndexToBytePos(origRunes, i)
		byteEnd := runeIndexToBytePos(origRunes, i+len(searchRunes))

		results = append(results, struct {
			byteStart int
			byteEnd   int
		}{byteStart: byteStart, byteEnd: byteEnd})
	}

	return results
}

// runeIndexToBytePos конвертирует индекс руны (в cleaned тексте) в байтовую позицию оригинала.
func runeIndexToBytePos(origRunes []rune, cleanIdx int) int {
	origIdx := 0
	cleanI := 0
	for origIdx < len(origRunes) && cleanI < cleanIdx {
		if !unicode.Is(unicode.Mn, origRunes[origIdx]) {
			cleanI++
		}
		origIdx++
	}

	bytePos := 0
	for i := 0; i < origIdx; i++ {
		bytePos += utf8.RuneLen(origRunes[i])
	}
	return bytePos
}

// findWordNormalized оставлен для совместимости, но теперь лучше использовать findAllWordNormalized
func findWordNormalized(text, word string) (int, int, bool) {
	all := findAllWordNormalized(text, word)
	if len(all) == 0 {
		return 0, 0, false
	}
	return all[0].byteStart, all[0].byteEnd, true
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
