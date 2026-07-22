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
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"syscall"

	"ZapretStratsTester/internal/domains"
	// TODO output
	"ZapretStratsTester/internal/output"
)

type Config struct {
	Threads       int
	NetTimeout    int
	Retries       int
	RetryDelay    int
	File          string
	PrintProgress bool
}

// На один воркер нужно минимум 4 файловых дескриптора
// Не упираемся в лимит OC, 1 запасной
const maxFilesPerWorker = 4 + 1

var config Config // Глобальные настройки программы

// getMaxOpenFiles запрашивает у ОС лимит на кол-во открытых файлов, при неудче возвращает 0
func getMaxOpenFiles() int {
	var rlim syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim)
	if err != nil {
		return 0 // Не можем определить макс. кол-во, не пройдет проверку, программа не запустится
	}
	return int(rlim.Cur)
}

func init() {
	// Settings
	var progress bool
	flag.BoolVar(&progress, "print-progress", false, "Отображение прогресс бара")
	file := flag.String("file", "./domains.txt", "Путь к файлу со списком доменов/айпи")
	threads := flag.Int("threads", 0, "Количество одновременных проверок"+
		"(0 - авто, расчитывается с учетом таймаута, ретраев, их задержки. Не превышает ulimit -n) Значение установленное вручную может привести к ошибкамиспользовать 0 ")
	netTimeout := flag.Int("net", 2, "Таймаут (сек) - время ожидания ответа от домена/айпи")
	retries := flag.Int("retries", 1, "Количество повторных попыток запроса к домену/айпи")
	retryDelay := flag.Int("retry-delay", 0, "Задержка между ретраями в мс. Умножается на номер попытки (0 - авто)")
	flag.Parse()

	// Если задержка не указана, используем 0 (domains подставит const)
	// В domains.CheckDomain передаем 0
	/*
		  if *retryDelay == 0 {
				*retryDelay = domains.DefaultRetryDelay
			}
	*/

	// Количество воркеров не превышает допустимое-количество-открытых-файлов/4, кроме случаев ручной установки значения
	osMaxFiles := getMaxOpenFiles()
	// Устанавливаем оптимальное количество воркеров, если не установлен флаг
	if *threads == 0 {
		if osMaxFiles < maxFilesPerWorker {
			// Слишком маленький лимит - запуск даже одного воркера может привести к ошибке
			fmt.Fprintf(os.Stderr, "ERROR: Лимит файлов (%d) слишком мал. Минимум: %d\n",
				osMaxFiles, maxFilesPerWorker)
			fmt.Fprintf(os.Stderr, "Рекомендуется: ulimit -n 1024 \n")
			os.Exit(1)
		}

		// База: ядра * 2
		workers := runtime.NumCPU() * 2

		// 1. Учитываем таймаут: чем больше таймаут, тем больше воркеров
		timeoutFactor := 1
		if *netTimeout > 10 {
			timeoutFactor = 3
		} else if *netTimeout > 3 {
			timeoutFactor = 2
		}
		workers *= timeoutFactor

		// 2. Учитываем ретраи: каждый ретрай увеличивает время ожидания
		// Если retries = 0, то фактор 1
		retryFactor := 1 + *retries/2 // 0->1, 1->1, 2->2, 3->2, 4->3, 5->3
		if retryFactor < 1 {
			retryFactor = 1
		}
		if retryFactor > 5 {
			retryFactor = 5
		}
		workers *= retryFactor

		// 3. Учитываем задержку между ретраями
		// Чем больше задержка, тем больше воркеров можно запустить
		delayFactor := 1
		// Задержка в секундах
		delaySec := float64(*retryDelay) / 1000.0
		// Логарифмическая шкала: delay 100ms -> +10%, 500ms -> +30%, 1000ms -> +50%
		if delaySec >= 0.1 && delaySec < 0.5 {
			delayFactor = 1 + int(delaySec*10)/10 // 0.1-0.4 -> 1.1-1.4
		} else if delaySec >= 0.5 && delaySec < 1.0 {
			delayFactor = 1 + int(delaySec*5)/10 // 0.5-0.9 -> 1.5-1.9
		} else if delaySec >= 1.0 {
			delayFactor = 2 + int(delaySec/2) // 1.0+ -> 2+
		}
		workers *= delayFactor

		// Минимальное и максимальное значение
		// Значение меньше 10 не устанавливается, кроме случаев, когда maxWorkers < 10 (см. ниже)
		if workers < 10 {
			workers = 10
		}

		// Никогда не может быть больше 200
		if workers > 200 {
			workers = 200
		}

		// На каждый домен-воркер создается максимум 4 одновременных коннекта, ретраи выполняются последовательно.
		maxWorkers := osMaxFiles / maxFilesPerWorker

		if workers > maxWorkers {
			workers = maxWorkers
		}
		*threads = workers

	} else {
		fmt.Fprintf(os.Stderr, "WARN: установленно нестандартное значение threads - %v"+
			"(максимально разрешенное ОС кол-во открытых файлов - %v, программа открывает не более %v на один thread)\n", *threads, osMaxFiles, maxFilesPerWorker)
	}

	config = Config{
		Threads:       *threads,
		NetTimeout:    *netTimeout,
		Retries:       *retries,
		RetryDelay:    *retryDelay,
		File:          *file,
		PrintProgress: progress,
	}
}

func main() {
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
	if config.PrintProgress {
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
	output.PrintTable(results)
}
