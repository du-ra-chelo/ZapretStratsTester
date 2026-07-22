package domains

import (
	"context"
	"net"
	"time"
)

// IsIP проверяет является ли строка IP-адресом
func IsIP(domain string) bool {
	return net.ParseIP(domain) != nil
}

// CheckIP проверяет доступность IP-адреса с ретраями
func CheckIP(ip string, timeout int, retries int, retryDelayMs int) Domain {
	result := Domain{
		Domain:      ip,
		Ping:        "FAIL",
		HTTP:        "N/A",
		TLS12:       "N/A",
		TLS13:       "N/A",
		IsAvailable: false,
	}
	timeoutDur := time.Duration(timeout) * time.Second

	// Если задержка не указана, используем значение по умолчанию
	if retryDelayMs <= 0 {
		retryDelayMs = DefaultRetryDelay
	}

	// Ограничиваем максимальную задержку
	if retryDelayMs > MaxRetryDelay {
		retryDelayMs = MaxRetryDelay
	}

	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			// Задержка с увеличением на каждой попытке
			delay := retryDelayMs * attempt
			if delay > MaxRetryDelay {
				delay = MaxRetryDelay
			}
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		ctx, cancel := context.WithTimeout(context.Background(),
			time.Duration(timeoutDur+2)*time.Second)
		defer cancel()
		// Сразу через системный ping
		result.Ping = systemPing(ctx, ip, timeoutDur).value

		if IsIPAvailable(result) {
			result.IsAvailable = true
			return result
		}
	}
	return result
}
