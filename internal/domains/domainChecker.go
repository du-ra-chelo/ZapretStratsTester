// Package domains проверяет доступность доменов и IP-адресов по протоколам HTTP, TLS 1.2 и TLS 1.3.
// Поддерживает параллельную проверку, ретраи и автоматическое определение IP-адресов.
package domains

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Константы для настройки ретраев
const (
	// DefaultRetryDelay базовая задержка между ретраями (в миллисекундах)
	DefaultRetryDelay = 150

	// MaxRetryDelay максимальная задержка между ретраями (в миллисекундах)
	MaxRetryDelay = 5000
)

type partialResult struct {
	typ        string // "ping", "http", "https"
	tlsVersion string // "1.2", "1.3"
	value      string
}

// CheckDomain проверяет доступность домена
// retries: количество повторных попыток (0 - только одна попытка)
// retryDelayMs: задержка между попытками в миллисекундах (0 - использовать DefaultRetryDelay)
func CheckDomain(domain string, timeout int, retries int, retryDelayMs int) Domain {
	if domain == "" {
		return Domain{Domain: domain, IsAvailable: false}
	}
	if IsIP(domain) {
		return CheckIP(domain, timeout, retries, retryDelayMs)
	}

	// Если задержка не указана, используем значение по умолчанию
	if retryDelayMs <= 0 {
		retryDelayMs = DefaultRetryDelay
	}

	// Ограничиваем максимальную задержку
	if retryDelayMs > MaxRetryDelay {
		retryDelayMs = MaxRetryDelay
	}

	var lastResult Domain
	for attempt := 0; attempt <= retries; attempt++ {
		// Задержка с увеличением на каждой попытке
		delay := retryDelayMs * attempt
		if delay > MaxRetryDelay {
			delay = MaxRetryDelay
		}
		// На первой (0) попытке ничего не делаем, только вызываем смену контекста
		time.Sleep(time.Duration(delay) * time.Millisecond)

		result := checkDomainOnce(domain, timeout)

		// Если успешно - возвращаем сразу
		if result.IsAvailable {
			return result
		}

		// Сохраняем последний результат
		lastResult = result
	}

	// Если все попытки неудачны - возвращаем последний результат
	return lastResult
}

// checkDomainOnce выполняет одну попытку проверки
func checkDomainOnce(domain string, timeout int) Domain {
	timeoutDur := time.Duration(timeout) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(timeoutDur+2)*time.Second)
	defer cancel()

	results := make(chan partialResult, 4)
	result := Domain{Domain: domain}

	// Запускаем проверки параллельно
	go func() {
		http, ping := checkHTTPAndPing(ctx, domain, timeoutDur)
		results <- http
		results <- ping
	}()
	go func() { results <- checkHTTPS(ctx, domain, timeoutDur, tls.VersionTLS12) }()
	go func() { results <- checkHTTPS(ctx, domain, timeoutDur, tls.VersionTLS13) }()

	var ping, http, tls12, tls13 string
	// Мы передали ctx в функции, нет смысла ждать ctx.Done()
	for i := 0; i < 4; i++ {
		r := <-results
		switch r.typ {
		case "ping":
			ping = r.value
		case "http":
			http = r.value
		case "https":
			if r.tlsVersion == "1.2" {
				tls12 = r.value
			} else if r.tlsVersion == "1.3" {
				tls13 = r.value
			}
		}
	}

	result.Ping = ping
	result.HTTP = http
	result.TLS12 = tls12
	result.TLS13 = tls13
	result.IsAvailable = IsDomenAvailable(domain, ping, tls12, tls13)

	return result
}

// checkHTTP отправляет http запрос,
// возвращает partialResult{typ:"http", value:"FAIL"|"HTTP:resp.StatusCode"} и partialResult{typ:"ping", value:"FAIL"|"time.Milliseconds()"}
func checkHTTPAndPing(ctx context.Context, domain string, timeoutDur time.Duration) (partialResult, partialResult) {
	pingRes := partialResult{typ: "ping", value: "FAIL"}
	httpRes := partialResult{typ: "http", value: "FAIL"}
	client := &http.Client{
		Timeout: timeoutDur,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("http://%s", domain), nil)
	// Ошибку проверяем, чтобы вызвать ping
	if err != nil {
		return httpRes, systemPing(ctx, domain, timeoutDur)
	}
	// ctx.Err() != nil считаем за FAIL

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		// FALLBACK OS ping
		if ctx.Err() != nil {
			return httpRes, pingRes
		} else {
			pingRes = systemPing(ctx, domain, timeoutDur)
		}
	} else {
		defer resp.Body.Close()
		httpRes.value = fmt.Sprintf("HTTP:%d", resp.StatusCode)
		pingRes.value = fmt.Sprintf("%v", elapsed.Milliseconds())
	}
	return pingRes, httpRes
}

// checkHTTPSVersion отправляет https запрос версии version, возвращает resp.StatusCode
func checkHTTPSVersion(ctx context.Context, domain string, timeout time.Duration, version uint16) int {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:         version,
				MaxVersion:         version,
				InsecureSkipVerify: true,
			},
			DisableKeepAlives: true,
			DialContext: (&net.Dialer{
				Timeout:   timeout,
				KeepAlive: 0,
			}).DialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, "HEAD", fmt.Sprintf("https://%s", domain), nil)
	if err != nil {
		return 0
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	return resp.StatusCode
}

// checkHTTPS оборачивает checkHTTPSVersion под нужды checkDomainOnce,
// возвращает partialResult{typ:"https", tlsVersion:"1.2"|"1.3", value:"FAIL"|"TLS:resp.StatusCode"}
func checkHTTPS(ctx context.Context, domain string, timeout time.Duration, tlsV uint16) partialResult {
	code := checkHTTPSVersion(ctx, domain, timeout, tlsV)

	var tlsVers, tlsResult string
	if tlsV == tls.VersionTLS12 {
		tlsVers = "1.2"
	} else {
		tlsVers = "1.3"
	}

	if code > 0 {
		tlsResult = fmt.Sprintf("TLS%s:%d", tlsVers, code)
	} else {
		tlsResult = "FAIL"
	}

	return partialResult{"https", tlsVers, tlsResult}
}
