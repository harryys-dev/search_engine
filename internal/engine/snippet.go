package engine

import (
	"github.com/blevesearch/bleve/v2/search"
	"sort"
	"strings"
	"unicode/utf8"
)

type ByteRange struct {
	Start int
	End   int
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

	// Сортируем интервалы по началу (чтобы правильно расставлять теги и находить первый)
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].Start < ranges[j].Start
	})

	firstMatch := ranges[0]
	bestIdx := firstMatch.Start

	// Ищем начало предложения (назад до 150 байт)
	start := bestIdx
	maxLookback := 150
	limit := 0
	if start > maxLookback {
		limit = start - maxLookback
	}

	for start > limit {
		if start > 0 {
			// Проверяем, не нарезали ли мы посередине многобайтового символа
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

	// Финальная проверка границы start на корректность UTF-8
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}

	// Пропускаем пробелы в начале
	for start < len(text) && (text[start] == ' ' || text[start] == '\t' || text[start] == '\n' || text[start] == '\r') {
		start++
	}

	// Отмеряем 250 байт от начала сниппета для конца
	end := start + 250
	if end >= len(text) {
		end = len(text)
	} else {
		// Стараемся отрезать по пробелу, чтобы не рубить слова
		// Сначала убеждаемся, что end не указывает в середину символа
		for end > start && !utf8.RuneStart(text[end]) {
			end--
		}

		spaceIdx := strings.LastIndex(text[start:end], " ")
		if spaceIdx > 120 {
			end = start + spaceIdx
		}
	}

	// Еще раз проверяем end на корректность UTF-8 (LastIndex мог вернуть середину, если пробел был однобайтовым, но мы отрезали по нему)
	// На самом деле slice [start:end] с LastIndex пробела безопасен, если пробел - ASCII.
	// Но для надежности:
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}

	// Проверяем, начинается ли сниппет с начала предложения или очень близко
	isStartOfLib := false
	if start == 0 {
		isStartOfLib = true
	} else if start > 0 {
		for i := start - 1; i >= 0; i-- {
			if text[i] != ' ' && text[i] != '\t' && text[i] != '\n' && text[i] != '\r' {
				if text[i] == '.' || text[i] == '!' || text[i] == '?' {
					isStartOfLib = true
				}
				break
			}
		}
	}

	var builder strings.Builder
	if !isStartOfLib && start > 0 {
		builder.WriteString("...")
	}

	// Вставляем маркеры с учетом рассчитанного диапазона [start:end]
	currentByte := start
	for _, r := range ranges {
		if r.End <= start {
			continue // Совпадение до нашего сниппета (бывает если есть много слов)
		}
		if r.Start >= end {
			break // Вышли за пределы сниппета
		}

		// Защита от выхода за границы
		mStart := r.Start
		if mStart < start {
			mStart = start
		}
		mEnd := r.End
		if mEnd > end {
			mEnd = end
		}

		// Добавляем текст до хайлайта
		if mStart > currentByte {
			builder.WriteString(escapeHTML(text[currentByte:mStart]))
		}

		// Избегаем наложения (когда одно слово заходит на другое)
		if mStart >= currentByte {
			builder.WriteString("<mark>")
			builder.WriteString(escapeHTML(text[mStart:mEnd]))
			builder.WriteString("</mark>")
			currentByte = mEnd
		}
	}

	// Добавляем оставшийся кусок текста до границы
	if currentByte < end {
		builder.WriteString(escapeHTML(text[currentByte:end]))
	}

	if end < len(text) {
		builder.WriteString("...")
	}

	return builder.String()
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
