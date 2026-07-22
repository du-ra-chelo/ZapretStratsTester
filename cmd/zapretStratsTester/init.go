package main

import (
	"flag"
)

type Config struct {
	domainsFile string
	stratsDir   string

	testerBin        string
	testerThreads    int
	testerNetTimeout int
	testerRetries    int
	testerRetryDelay int

	zapretThreads int
}

var cfg Config

func init() {
	domainsFile := flag.String("domains", "./domains.txt", "Список доменов для проверки")
	stratsDir := flag.String("strats", "/opt/zapret/zapret.cfgs/configurations", "Путь к папке со стратегиями zapret")

	// Возможность использовать сторонние tester?
	// На данный момент поддерживается только areAvailable
	testerBin := flag.String("tester", "./areAvailable", "Путь к программe проверки доступности")
	testerThreads := flag.Int("tester-threads", 0, "Кол-во одновременных потоков опроса программы проверки. 0 - авто")
	testerNetTimeout := flag.Int("tester-timeout", 0, "Время ожидания ответа домена. 0 - авто")
	testerRetries := flag.Int("tester-retries", 0, "Кол-во повторных запросов к домену. 0 - авто")
	testerRetryDelay := flag.Int("tester-retry-delay", 0, "Задержка между повторными запросами, растет с номером попытки. 0 - авто")

	zapretThreads := flag.Int("zapret-threads", 3, "Кол-во одновременно запущенных экземпляров zapret")
	flag.Parse()

	cfg = Config{
		domainsFile: *domainsFile,
		stratsDir:   *stratsDir,

		testerBin:        *testerBin,
		testerThreads:    *testerThreads,
		testerNetTimeout: *testerNetTimeout,
		testerRetries:    *testerRetries,
		testerRetryDelay: *testerRetryDelay,

		zapretThreads: *zapretThreads,
	}
}
