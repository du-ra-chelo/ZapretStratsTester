// ZapretStratsTester - программа для параллельного тестирования стратегий на разных экземплярах zapret (nfqws)
// В качестве тестера предусмотрен модуль areAvailable, отправляющий параллельные запросы на домены из списка
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"ZapretStratsTester/internal/domains"
	"ZapretStratsTester/internal/firewall"
	"ZapretStratsTester/internal/nfqws"
	"ZapretStratsTester/internal/osutil"
)

const (
	cgroupSliceName  = "ZST"
	cgroupScopeName  = "ZST-tester-%d" // + 1...15
	scopePathPattern = "/%s.slice/%s.scope"
	readyFilePath    = "/tmp/nftables-ready"
	tmpDomainsName   = "ZST-domains-qnum%d"
	tmpDomainsDir    = "/tmp"
	nfqwsStartQnum   = 201
)

const (
	ExitSuccess = iota
	ExitGeneralError
	ExitUsage
	ExitConfig
	ExitNetwork
	ExitPermission
	ExitIO
	ExitDependency
)

var StratsArray = []string{"/opt/zapret/zapret.cfgs/configurations/discord", "/opt/zapret/zapret.cfgs/configurations/UltimateFix", "/opt/zapret/zapret.cfgs/configurations/general_ALT10"}

var OsExit = ExitSuccess

func main() {
	// defer для установки кода завершения.
	// Вместо log.Fatal используется fatal() - обертка над fmt.Fprintf(os.Stderr, ...) + установка osExit
	defer func() {
		os.Exit(OsExit)
	}()
	// Проверка прав пользователя
	if os.Geteuid() != 0 {
		fatal(ExitPermission, "Ошибка: требуется запуск с правами root")
		return
	}
	// Наличие зависимостей: zapret, tester, nftables|iptables
	if err := checkDeps(); err != nil {
		fatal(ExitDependency, "Ошибка: не обнаружены необходимые зависимости: %s\n", err)
		return
	}
	// Пробуем завершить системный Zapret, если тот запущен
	if active, err := osutil.IsServiceActive("zapret"); err != nil {
		// Серивис не существует или непредвиденная ошибка systemctl
		fatal(ExitGeneralError, "Ошибка: не удалось проверить статус zapret.service: %s\n", err)
		return
	} else if active {
		if err := osutil.Systemctl("stop", "zapret"); err != nil { // Пробуем остановить
			fatal(ExitGeneralError, "Ошибка: не удалось завершить zapret.service: %s\n", err)
			return
		}
		defer osutil.Systemctl("start", "zapret") // По окончании работы восстанавливаем состояние
		defer fmt.Println("Запуск запрета")
	}

	// Сохраняем таблицу nft во временный файл
	// TODO: iptables
	if err := firewall.NftablesSave(); err != nil {
		fatal(ExitGeneralError, "Ошибка: нe удалось создать бэкап таблицы: %s\n", err)
		return
	}
	defer firewall.NftablesRecover() // Таблица восстановится ДО перезапуска zapret
	defer fmt.Println("Восстановление nftables")
	if err := firewall.NftablesClear(); err != nil {
		fatal(ExitGeneralError, "Ошибка: не удалось очистить правила: %s", err)
		return
	}

	// Запускаем testers и nfqws procs
	type Tester struct {
		Cgroup osutil.Cgroup
		Req    nfqws.Req
		Result []domains.Domain // json
	}

	var wg sync.WaitGroup
	stratsAll, err := os.ReadDir(cfg.stratsDir)
	if err != nil {
		fatal(ExitGeneralError, "Ошибка: не удалось прочитать стратегии из директории %s: %s\n", cfg.stratsDir, err)
		return
	}

	resultCh := make(chan Tester, len(stratsAll))
	nfqwsInstanses := make(chan struct{}, cfg.zapretThreads) // Семафор
	nfqwsReqCh := make(chan nfqws.Req, cfg.zapretThreads)
	nfqwsErrCh := make(chan error)

	nfqwsPath := getNfqwsBinPath()
	manager := nfqws.NewManager(nfqwsPath, nfqwsReqCh, nfqwsErrCh)
	manager.Start()
	defer func() {
		go func() {
			for err := range nfqwsErrCh {
				if err != nil {
					fmt.Println("Ошбка завершения nfqws", err)
				}
			}
		}()
		manager.Stop()
	}()
	defer fmt.Println("Остановка manager")

	getUnDomainsFile, err := uniqueFileGen(tmpDomainsDir, tmpDomainsName)
	if err != nil {
		fatal(ExitIO, "Ошибка при открытии/чтении файла с доменами: %s", err)
		return
	}

	// Остальные тестеры будут заменять выполнившиеся, не меняя имени
	scopesNames := make([]string, 0, cfg.zapretThreads)
	for q := range cfg.zapretThreads {
		qnum := nfqwsStartQnum + q
		scopesNames = append(scopesNames, fmt.Sprintf(cgroupScopeName, qnum)) // Индекс scope name == qnum-nfqwsStartQnum

		unDomainsFile := getUnDomainsFile(qnum) // Создаем разные файлы с доменами для каждого тестера
		if unDomainsFile == "" {
			fatal(ExitIO, "Ошибка: не удалось заполнить доменами временный файл в %s: %s", tmpDomainsDir, err)
			return
		}

		tester := Tester{Req: nfqws.Req{Queue: qnum, Args: StratsArray[q]}}

		nfqwsInstanses <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-nfqwsInstanses }()

			// Запрашиваем у manager запуск nfqws, ждем результат
			nfqwsReqCh <- tester.Req
			if err := <-nfqwsErrCh; err != nil {
				fatal(ExitGeneralError, "Ошибка: не удалось запустить новый процесс nfqws: %s", err)
				return
			}
			// Создаем cgroup
			// -with-file - запускаем только после генерации таблицы
			tester.Cgroup = osutil.NewCGroupScope(cgroupSliceName, scopesNames[q],
				cfg.testerBin, "-file", unDomainsFile, "-with-file", readyFilePath, "-file-timeout", "20")
			if tester.Cgroup.Err != nil {
				fatal(ExitGeneralError, "Ошибка: не удалось запустить процесс tester в cgroup: %s",
					tester.Cgroup.Err)
				return
			}
			fmt.Printf("Cgroup создана, tester: \n%v\n%v\n", tester.Cgroup.Slice, tester.Req.Args)
			resultCh <- tester
		}()
	}
	// В случае остановки программы все процессы останавливаются, слайс удаляется
	defer osutil.KillCGroup(osutil.CGroupLynxSystemdPath, cgroupSliceName)
	defer fmt.Println("Остановка cgroup")

	err = firewall.NftablesApply(nftTablePattern)
	if err != nil {
		fatal(ExitGeneralError, "Ошибка: не удалось применить шаблон nftables: %s\n", err)
		return
	}
	// Ждем создания всех cgroup
	func() {
		lastScope := fmt.Sprintf(scopePathPattern,
			cgroupSliceName, scopesNames[len(scopesNames)-1]) // Длина не меньше 1
		lastScopePath := filepath.Join(osutil.CGroupLynxSystemdPath, lastScope)
		for range 6 { // 5 сек
			if err := osutil.IsFileExist(lastScopePath, ""); err == nil {
				fmt.Println("Последний тестер создан")
				break
			}
			time.Sleep(1 * time.Second)
			fmt.Println("Жду создания тестера: ", lastScopePath)
		}
	}()
	// Дополняем таблицу правилами перенаправления трафика
	err = nftableGenRules(scopesNames)
	if err != nil {
		fatal(ExitGeneralError, "Ошибка: не удалось установить временные правила: %s\n", err)
		return
	}
	// TEST
	// nft
	cmd := exec.Command("nft", "list table inet ZST")
	out, _ := cmd.CombinedOutput()
	fmt.Println("Правила: ", string(out))
	// nfqws
	psOut, err := exec.Command("ps", "aux").Output()
	if err != nil {
		panic(err)
	}
	fmt.Println("nfqws:")
	rg := exec.Command("rg", "nfqws")
	rg.Stdin = bytes.NewReader(psOut)
	result, err := rg.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				fmt.Println("Ничего не найдено")
				return
			}
		}
		panic(err)
	}
	fmt.Printf("Res: %v\n", string(result))
}

func fatal(exitCode int, msg string, args ...any) {
	fmt.Fprintf(os.Stderr, msg, args...)
	OsExit = exitCode
}

// checkDeps проверяет наличие необходимых файлов и программ в системе
func checkDeps() error {
	// TODO: дополнить проверку необходимых программ
	// Проверяем наличие zapret1 в сиситеме
	if err := osutil.IsFileExist(cfg.zapretFolder,
		"установите zapret или укажите верный путь"); err != nil {
		return err
	}
	// Проверяем существует ли программа-тестер
	if err := osutil.IsFileExist(cfg.testerBin,
		"программу или укажите верный путь"); err != nil {
		return err
	}

	// Проверяем наличие firewall
	// По дефолту nftables
	// TODO: поддержка iptables и чтение используемого файервола из конфига zapret
	if err := osutil.IsInstalled("nft"); err != nil {
		return fmt.Errorf("не удалось запустить nftables: %w", err)
	}
	return nil
}

// nftableGenRules создает правила в таблице nftables для обработки не более 15 экзэмпляров cgroup,
// в противном случае произойдет переполнение маски 0x0F000000
func nftableGenRules(scopesNames []string) error {
	if len(scopesNames) > 15 {
		return fmt.Errorf("правила не созданы: риск переполнения metaMark")
	}
	var metaMarkCG uint32 = metaMarkStep
	queue := startQueueNum
	for _, scopeName := range scopesNames {
		cgroup := fmt.Sprintf(scopePathPattern,
			cgroupSliceName, scopeName)
		rule := fmt.Sprintf(nftRuleOutputTemplate,
			cfg.wanIface, cgroup, metaMarkCG) // Праивло маркировки трафика процессов cgroup
		// Marker
		err := firewall.NftablesExec(rule)
		if err != nil {
			return fmt.Errorf("ошибка создания правила маркировки cgroup: %w", err)
		}
		// TCP
		rule = fmt.Sprintf(nftRulePostnatTemplate,
			cfg.wanIface, metaMarkCG, nftTCP, queue) // Правило перенаправления tcp трафика в queue
		err = firewall.NftablesExec(rule)
		if err != nil {
			return fmt.Errorf("ошибка создания правила маршрутизации tcp cgroup: %w", err)
		}
		// UDP
		rule = fmt.Sprintf(nftRulePostnatTemplate,
			cfg.wanIface, metaMarkCG, nftUDP, queue) // Правило перенаправления udp трафика в queue
		err = firewall.NftablesExec(rule)
		if err != nil {
			return fmt.Errorf("ошибка создания правила маршрутизации udp cgroup: %w", err)
		}
		metaMarkCG += metaMarkStep
		queue++
	}
	return nil
}

func getNfqwsBinPath() string {
	// TODO: автоопределение бинарника для каждой платформы
	return filepath.Join(cfg.zapretFolder, "binaries/linux-x86_64/nfqws")
}

// uniqueFileGen - через замыкание возвращает функцию, которая созает файл с названием требуемого формата в переданной дирректории.
// Созданные файлы содержат одинаковые данные из cfg.domainsFile, но в разном порядке
func uniqueFileGen(dir, fFmt string) (func(...any) string, error) {
	domainsList, err := os.Open(cfg.domainsFile)
	if err != nil {
		return nil, err
	}
	defer domainsList.Close()
	// Читаем все строки и добавляем в слайс
	lines := make([]string, 0)
	reader := bufio.NewReader(domainsList)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		lines = append(lines, line)
	}

	// TODO: множитель зависит от нужного кол-ва единовременных файлов
	startLine := 0
	step := len(lines) * 10 / 100
	return func(fargs ...any) string {
		// В случае ошибки функция вернет пустую строку
		if len(lines) == 0 {
			return ""
		}
		// Копируем строки
		ll := make([]string, 0)
		ll = append(lines[startLine:len(lines)-1], lines[0:startLine]...)
		startLine += step
		if startLine > len(lines) {
			startLine -= len(lines)
		}
		// Создаем файл
		fName := fmt.Sprintf(fFmt, fargs...)
		fPath := filepath.Join(dir, fName)
		f, err := os.OpenFile(fPath, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return ""
		}
		defer f.Close()
		// Переносим строки
		writer := bufio.NewWriter(f)
		for _, l := range ll {
			if _, err := writer.WriteString(l); err != nil {
				return ""
			}
		}
		if err := writer.Flush(); err != nil {
			return ""
		}
		return fPath
	}, nil
}
