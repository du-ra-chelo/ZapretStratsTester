// Package domains проверяет доступность доменов и IP-адресов по протоколам HTTP, TLS 1.2 и TLS 1.3. Поддерживает параллельную проверку, ретраи и автоматическое определение IP-адресов.
package domains

import "strings"

type Domain struct {
	Domain      string
	Ping        string
	HTTP        string
	TLS12       string
	TLS13       string
	IsAvailable bool
}

// IsDomenAvailable определяет доступность домена или IP-адреса.
//   - IP-адрес считается доступным, если ping прошел
//   - Домен считается доступным, если TLS 1.2 или TLS 1.3 вернули успешный код
//   - HTTP ответ НЕ учитывается при определении доступности домена
func IsDomenAvailable(domain, ping, tls12, tls13 string) bool {
	// Проверяем общий статус через ping
	if ping == "FAIL" || ping == "N/A" || ping == "" {
		return false
	}

	// Если это IP-адрес - доступен
	if IsIP(domain) {
		return true
	}

	// Для домена - проверяем TLS статусы
	// Успешные коды: 2xx и 3xx
	isSuccess := func(status string) bool {
		if status == "FAIL" || status == "N/A" || status == "" {
			return false
		}
		// Формат: "TLS1.2:200" или "TLS1.3:301"
		parts := strings.Split(status, ":")
		if len(parts) != 2 {
			return false
		}
		code := parts[1]
		if len(code) == 0 {
			return false
		}
		// Проверяем что код начинается с 2 или 3 (2xx или 3xx)
		return code[0] == '2' || code[0] == '3'
	}

	return isSuccess(tls12) || isSuccess(tls13)
}

func IsIPAvailable(ip Domain) bool {
	if ip.Ping != "FAIL" && ip.Ping != "N/A" && ip.Ping != "" {
		return true
	}
	return false
}
