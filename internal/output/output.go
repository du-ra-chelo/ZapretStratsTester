// Package output выводит результаты проверки в виде форматированной таблицы со статистикой.
package output

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"ZapretStratsTester/internal/domains"
)

func PrintProgress(now, max int) {
	percent := now * 100 / max
	progress := fmt.Sprintf("%d%% (%d/%d)", percent, now, max)
	line := strings.Repeat("-", percent/5) + ">"
	fmt.Printf("[%-20s] %s\r", line, progress)
}

// PrintTable выводит результаты в виде таблицы
func PrintTable(results []domains.Domain) {
	if len(results) == 0 {
		fmt.Println("Нет результатов для отображения")
		return
	}

	// Определяем максимальную ширину колонки домена
	maxDomain := 20
	for _, r := range results {
		domainLen := utf8.RuneCountInString(r.Domain)
		if domainLen > maxDomain {
			maxDomain = domainLen
		}
	}
	if maxDomain < 10 {
		maxDomain = 10
	}

	// Ширина колонок
	colWidths := []int{maxDomain, 6, 8, 8, 10, 10}
	headers := []string{"Домен/IP", "Статус", "Ping", "HTTP", "TLS 1.2", "TLS 1.3"}

	// Вычисляем общую ширину таблицы (только символы-разделители)
	// Формат: "│ %-*s │ %-*s │ ... │"
	// Каждая колонка: пробел + значение + пробел + "│"
	totalWidth := 1 // начальная "│"
	for i, w := range colWidths {
		totalWidth += w + 3 // ширина_колонки + пробел_слева + пробел_справа + "│"
		if i == len(colWidths)-1 {
			totalWidth-- // у последней колонки убираем лишний пробел после значения
		}
	}

	// Функция для создания разделительной линии
	makeSep := func(left, mid, right string) string {
		parts := make([]string, len(colWidths))
		for i, w := range colWidths {
			parts[i] = strings.Repeat("─", w+2) // w + 2 пробела вокруг значения
		}
		return left + strings.Join(parts, mid) + right
	}

	// Заголовок
	fmt.Println(makeSep("┌", "┬", "┐"))

	fmt.Print("│")
	for i, h := range headers {
		if i < len(headers)-1 {
			fmt.Printf(" %-*s │", colWidths[i], h)
		} else {
			fmt.Printf(" %-*s │", colWidths[i], h)
		}
	}
	fmt.Print("\n")

	fmt.Println(makeSep("├", "┼", "┤"))

	// Данные
	for _, r := range results {
		status := "✗ FAIL"
		if r.IsAvailable {
			status = "✓ OK"
		}

		values := []string{
			r.Domain,
			status,
			r.Ping,
			r.HTTP,
			r.TLS12,
			r.TLS13,
		}

		fmt.Print("│")
		for i, v := range values {
			fmt.Printf(" %-*s │", colWidths[i], truncateByRune(v, colWidths[i]))
		}
		fmt.Print("\n")
	}

	// Статистика
	total := len(results)
	available := 0
	for _, r := range results {
		if r.IsAvailable {
			available++
		}
	}

	fmt.Println(makeSep("└", "┴", "┘"))
	fmt.Println(makeSep("├", "─", "┤"))

	statsText := fmt.Sprintf(" Доступно: %d/%d доменов/IP ", available, total)
	// Вычисляем отступ справа
	statsLen := utf8.RuneCountInString(statsText)
	padding := totalWidth - statsLen - 1
	if padding < 0 {
		padding = 0
	}
	fmt.Printf("│%s%s│\n", statsText, strings.Repeat(" ", padding))

	fmt.Println(makeSep("└", "─", "┘"))
	fmt.Println()
	fmt.Println(available)
}

// truncateByRune обрезает строку до указанной длины с учётом рун (Unicode символов)
func truncateByRune(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
