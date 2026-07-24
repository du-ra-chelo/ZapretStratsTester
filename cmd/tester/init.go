package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"syscall"
)

type Config struct {
	Threads int
	File    string

	NetTimeout int
	Retries    int
	RetryDelay int

	WithFile    string
	FileTimeout int

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
	file := flag.String("file", "./domains.txt", "Путь к файлу со списком доменов/айпи")
	threads := flag.Int("threads", 0, "Количество одновременных проверок"+
		"(0 - авто, расчитывается с учетом таймаута, ретраев, их задержки. Не превышает ulimit -n) Значение установленное вручную может привести к ошибкамиспользовать 0 ")

	netTimeout := flag.Int("net", 2, "Таймаут (сек) - время ожидания ответа от домена/айпи")
	retries := flag.Int("retries", 1, "Количество повторных попыток запроса к домену/айпи")
	retryDelay := flag.Int("retry-delay", 0, "Задержка между ретраями в мс. Умножается на номер попытки (0 - авто)")

	flag.BoolVar(&progress, "print-progress", false, "Отображение прогресс бара")

	withFile := flag.String("with-file", "", "Если указано значение, работа не начнется до появления файла-флага")
	fileTimeout := flag.Int("file-timeout", 5, "Таймаут ожидания (сек) значения из сокета (см. with-file)")
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
		fmt.Fprintf(os.Stderr, "ВНИМАНИЕ: установленно нестандартное значение threads - %v"+
			"(максимально разрешенное ОС кол-во открытых файлов - %v, программа открывает не более %v на один thread)\n", *threads, osMaxFiles, maxFilesPerWorker)
	}

	config = Config{
		Threads: *threads,
		File:    *file,

		NetTimeout: *netTimeout,
		Retries:    *retries,
		RetryDelay: *retryDelay,

		WithFile:    *withFile,
		FileTimeout: *fileTimeout,

		PrintProgress: progress,
	}
}
