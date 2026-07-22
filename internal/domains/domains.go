// Package domains проверяет доступность доменов и IP-адресов по протоколам HTTP, TLS 1.2 и TLS 1.3. Поддерживает параллельную проверку, ретраи и автоматическое определение IP-адресов.
package domains

type Domain struct {
	Domain      string
	Ping        string
	HTTP        string
	TLS12       string
	TLS13       string
	IsAvailable bool
}

// IsAvailable определяет доступность домена или IP-адреса.
func IsAvailable(domain, ping, http, tls12, tls13 string) bool {
	// Если это IP-адрес - доступен
	if IsIP(domain) {
		return ping != "FAIL" && ping != "N/A" && ping != ""
	}

	// Для домена - любой успешный ответ кроме ping
	if http != "FAIL" && http != "N/A" && http != "" {
		return true
	}
	if tls12 != "FAIL" && tls12 != "N/A" && tls12 != "" {
		return true
	}
	if tls13 != "FAIL" && tls13 != "N/A" && tls13 != "" {
		return true
	}
	return false
}
