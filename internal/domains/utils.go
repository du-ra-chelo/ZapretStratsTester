package domains

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ReadDomains читает домены из файла, лишние пробелы и комментарии # удаляются. Возвращает массив строк-доменов
func ReadDomains(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var domains []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Удаляем комментарии
		if idx := strings.Index(line, "#"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		// Пропускаем пустые
		if line == "" {
			continue
		}
		domains = append(domains, line)
	}

	return domains, scanner.Err()
}

func systemPing(ctx context.Context, domain string, timeoutDur time.Duration) partialResult {
	timeoutSec := int(timeoutDur.Seconds())
	if timeoutSec < 1 {
		timeoutSec = 1
	}

	// Отправляем 1 пакет для скорости
	cmd := exec.CommandContext(
		ctx, "ping",
		"-c", "1",
		"-W", fmt.Sprintf("%d", timeoutSec), // таймаут
		domain,
	)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	err := cmd.Run()
	output := stdout.String()

	result := partialResult{typ: "ping", value: "FAIL"}

	if ctx.Err() != nil {
		return result // FAIL
	}

	if err != nil {
		// С ошибкой редко возвращается частичный успех
		return result // FAIL
	}

	// Успешный ping
	if strings.Contains(output, "bytes from") && strings.Contains(output, "time=") {
		if avg := extractTimeFromLine(output); avg > 0 {
			result := partialResult{typ: "ping", value: fmt.Sprintf("%dms", avg)}
			return result
		}
	}

	return result // FAIL
}

// extractTimeFromLine извлекает время из строки "time=0.045 ms"
func extractTimeFromLine(output string) int {
	// Ищем первую строку с time=
	lines := strings.Split(output, "\n")
	// В первой строке нет пинга
	for _, line := range lines[1:] {
		if strings.Contains(line, "time=") {
			parts := strings.Split(line, "time=")
			if len(parts) >= 2 {
				// Берем число до пробела
				timeStr := strings.Fields(parts[1])[0]
				timeFloat, err := strconv.ParseFloat(timeStr, 64)
				if err == nil {
					return int(timeFloat)
				}
			}
		}
	}
	return 0
}
