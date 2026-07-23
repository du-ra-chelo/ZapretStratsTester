package main

import (
	"flag"
)

const (
	defaultPathTester = "./tester"
	defaultPathZapret = "/opt/zapret"
)

var cfg Config

type Config struct {
	domainsFile string
	stratsDir   string

	testerBin        string
	testerThreads    int
	testerNetTimeout int
	testerRetries    int
	testerRetryDelay int

	zapretFolder  string
	zapretThreads int

	wanIface string
}

// Реализация flag.Value
type stringValue string

func (s *stringValue) String() string { return string(*s) }
func (s *stringValue) Set(v string) error {
	*s = stringValue(v)
	return nil
}

func init() {
	domainsFile := flag.String("domains", "./domains.txt", "Список доменов для проверки")
	stratsDir := flag.String("strats", "/opt/zapret/zapret.cfgs/configurations", "Путь к папке со стратегиями zapret")

	// Возможность использовать сторонние tester?
	// На данный момент поддерживается только areAvailable
	var testerBin stringValue = defaultPathTester
	flag.Var(&testerBin, "tester", "Путь к программe проверки доступности (default "+defaultPathTester+")")
	flag.Var(&testerBin, "t", "Путь к программe проверки доступности (default "+defaultPathTester+")")
	testerThreads := flag.Int("tester-threads", 0, "Кол-во одновременных потоков опроса программы проверки. 0 - авто")
	testerNetTimeout := flag.Int("tester-timeout", 0, "Время ожидания ответа домена. 0 - авто")
	testerRetries := flag.Int("tester-retries", 0, "Кол-во повторных запросов к домену. 0 - авто")
	testerRetryDelay := flag.Int("tester-retry-delay", 0, "Задержка между повторными запросами, растет с номером попытки. 0 - авто")

	var zapretFolder stringValue = defaultPathZapret
	flag.Var(&zapretFolder, "zapret", "Путь к папке zapret (default "+defaultPathZapret+")")
	flag.Var(&zapretFolder, "z", "Путь к папке zapret (default "+defaultPathZapret+")")
	zapretThreads := flag.Int("zapret-threads", 3, "Кол-во одновременно запущенных экземпляров zapret")

	wanIface := flag.String("wan", "wlan0", "Имя wan интерфейса для выхода в интернет")
	flag.Parse()

	cfg = Config{
		domainsFile: *domainsFile,
		stratsDir:   *stratsDir,

		testerBin:        testerBin.String(),
		testerThreads:    *testerThreads,
		testerNetTimeout: *testerNetTimeout,
		testerRetries:    *testerRetries,
		testerRetryDelay: *testerRetryDelay,

		zapretFolder:  zapretFolder.String(),
		zapretThreads: *zapretThreads,

		wanIface: *wanIface,
	}
}
