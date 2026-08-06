package main

import (
	"github.com/spf13/pflag"
)

const (
	defaultPathTester  = "./tester"
	defaultPathZapret  = "/opt/zapret"
	defaultStratsDir   = defaultPathZapret + "/zapret.cfgs/configurations"
	defaultDomainsFile = "./domains.txt"
	defaultWanIface    = "wlan0"
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

func init() {
	var domainsFile string
	pflag.StringVarP(&domainsFile, "domains", "d", defaultDomainsFile, "Список доменов для проверки")

	// Возможность использовать сторонние tester?
	// На данный момент поддерживается только areAvailable
	var testerBin string
	pflag.StringVarP(&testerBin, "tester", "t", defaultPathTester, "Путь к программe проверки доступности")
	testerThreads := pflag.Int("tester-threads", 0, "Кол-во одновременных потоков опроса программы проверки. 0 - авто")
	testerNetTimeout := pflag.Int("tester-timeout", 0, "Время ожидания ответа домена. 0 - авто")
	testerRetries := pflag.Int("tester-retries", 0, "Кол-во повторных запросов к домену. 0 - авто")
	testerRetryDelay := pflag.Int("tester-retry-delay", 0, "Задержка между повторными запросами, растет с номером попытки. 0 - авто")

	var zapretFolder string
	pflag.StringVarP(&zapretFolder, "zapret", "z", defaultPathZapret, "Путь к папке zapret")
	// TODO: авто установка кол-ва потоков
	// TODO: валидация флагов
	// TODO: автоопределение кол-ва потоков zapret. Кол-ва потоков tester?
	zapretThreads := pflag.Int("zapret-threads", 3, "Кол-во одновременно запущенных экземпляров zapret")

	var stratsDir string
	pflag.StringVarP(&stratsDir, "strats", "s", defaultStratsDir, "Путь к папке со стратегиями zapret")

	wanIface := pflag.String("wan", defaultWanIface, "Имя wan интерфейса для выхода в интернет")
	pflag.Parse()

	cfg = Config{
		domainsFile: domainsFile,
		stratsDir:   stratsDir,

		testerBin:        testerBin,
		testerThreads:    *testerThreads,
		testerNetTimeout: *testerNetTimeout,
		testerRetries:    *testerRetries,
		testerRetryDelay: *testerRetryDelay,

		zapretFolder:  zapretFolder,
		zapretThreads: *zapretThreads,

		wanIface: *wanIface,
	}
}
