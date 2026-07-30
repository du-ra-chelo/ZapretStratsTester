// areAvailable - программа параллельного опроса доменов для проверки доступности.
//
//	Проверяет доступ через системный ping и по протоколам HTTP, TLS1.2, TLS1.3
//
//	IP считается доступным при получении ответа ping, другие проверки не проводятся
//	Домен считается доступным при получении любого ответа через ПРОТОКОЛЫ (ping не учитывается)
//
// Флаги позволяют выбрать время ожидания ответа, задержку между повторными запросами,
// кол-во одновременно опрашиваемых доменов и повторных попыток
package main

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"ZapretStratsTester/internal/domains"
	"ZapretStratsTester/internal/output"
)

func main() {
	// Ждем сигнала от программы-родителя, при неудаче завершаем программу ошибкой
	if config.WithFile != "" {
		withFile()
	}
	// Список доменов
	domainsList, err := domains.ReadDomains(config.File)
	if err != nil {
		log.Fatal(err)
	}

	// Для вывода в одинаковом порядке результаты вставляются в слайс по result.Index
	// Результаты выводятся после получения len(domainsList) результатов
	type IndexedResult struct {
		Index          int
		domains.Domain // Структура с результатами тестирования
	}
	var workersWG sync.WaitGroup
	workers := make(chan struct{}, config.Threads)            // Семафор, чтобы воркеры не привышали лимит Threads
	resultChan := make(chan *IndexedResult, len(domainsList)) // Канал для отправки результатов

	readChan := resultChan // Чан для чтения результатов, может быть подменен прогресс баром

	// Для вывода строки прогресса
	// resultChan используется прогресс баром для чтения, сортировка читает из progressOut
	var progressWG *sync.WaitGroup
	if config.Progress {
		var wg sync.WaitGroup
		progressWG = &wg
		progressOut := make(chan *IndexedResult, len(domainsList))

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(progressOut)
			// Выводим кол-во завершенных воркеров
			var finishedCounter int
			for r := range resultChan {
				output.PrintProgress(finishedCounter, len(domainsList))
				finishedCounter++
				progressOut <- r
			}
		}()
		// Сортируем результаты после того, как их прочитает прогресс-бар
		readChan = progressOut
	}

	for idx, domain := range domainsList {
		workersWG.Add(1)
		workers <- struct{}{}
		go func(i int, d string) {
			defer workersWG.Done()
			defer func() { <-workers }()

			resultChan <- &IndexedResult{
				i,
				domains.CheckDomain(d,
					config.NetTimeout, config.Retries, config.RetryDelay),
			}
		}(idx, domain)
	}

	go func() {
		workersWG.Wait()
		close(resultChan) // Сигнал, что больше результатов не будет
	}()

	// Сортируем
	results := make([]domains.Domain, len(domainsList))
	for res := range readChan {
		results[res.Index] = res.Domain
	}

	// Завершаем прогресс бар рутину
	if progressWG != nil {
		progressWG.Wait()
	}
	printResults(results)
}

func withFile() {
	sleepTime := 100 * time.Millisecond
	for t := 0; t < config.FileTimeout*10; t++ {
		if _, err := os.Stat(config.WithFile); err == nil {
			return
		}
		time.Sleep(sleepTime)
	}
	log.Fatal("Время ожидания файла-сигнала начала прогроммы истекло")
}

// printResults вызывает функцию вывода результатов, основываясь на наличии выходного формата в map.
// При некорректном формате исползуется defaultOutputFormat
func printResults(results []domains.Domain) {
	if fn, ok := outputFormats[config.OutputFormat]; ok {
		fn(results)
		return
	}
	fmt.Fprintf(os.Stderr, "Неверный формат: %s, использую %s\n", config.OutputFormat, defaultOutputFormat)
	if fn, ok := outputFormats[defaultOutputFormat]; ok {
		fn(results)
		return
	}
	panic("функции для форматирования в defaultOutputFormat не существует")
}
