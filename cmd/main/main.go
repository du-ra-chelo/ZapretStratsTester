// ZapretStratsTester - программа для параллельного тестирования стратегий на разных экземплярах zapret (nfqws)
// В качестве тестера предусмотрен модуль areAvailable, отправляющий параллельные запросы на домены из списка
package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"ZapretStratsTester/internal/domains"
	"ZapretStratsTester/internal/firewall"
	"ZapretStratsTester/internal/nfqws"
	"ZapretStratsTester/internal/osutil"
	"ZapretStratsTester/internal/recover"
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

var (
	Recoverer = recover.NewRecoverer()
	OsExit    = ExitSuccess
)

const PanicSIGINT = "SIGINT"

func main() {
	// Выполняется только при удачном завершении программы.
	// В ином случае Fatal вызывает эту функцию
	defer ProgramEnd()

	// TODO: перехват SIGINT

	// Проверка прав пользователя
	if os.Geteuid() != 0 {
		Fatal(ExitPermission, "Ошибка: требуется запуск с правами root")
	}
	// Наличие зависимостей: zapret, tester, nftables|iptables
	if err := checkDeps(); err != nil {
		Fatal(ExitDependency, "Ошибка: не обнаружены необходимые зависимости: %s\n", err)
	}
	// Пробуем завершить системный Zapret, если тот запущен
	if active, err := osutil.IsServiceActive("zapret"); err != nil {
		// Серивис не существует или непредвиденная ошибка systemctl
		Fatal(ExitGeneralError, "Ошибка: не удалось проверить статус zapret.service: %s\n", err)
	} else if active {
		if err := osutil.Systemctl("stop", "zapret"); err != nil { // Пробуем остановить
			Fatal(ExitGeneralError, "Ошибка: не удалось завершить zapret.service: %s\n", err)
		}
		// Отложенный запуск
		Recoverer.Add(func() error { return osutil.Systemctl("start", "zapret") })
	}

	// Сохраняем таблицу nft во временный файл
	// TODO: iptables
	if err := firewall.NftablesSave(); err != nil {
		Fatal(ExitGeneralError, "Ошибка: нe удалось создать бэкап таблицы: %s\n", err)
	}
	Recoverer.Add(firewall.NftablesRecover)
	if err := firewall.NftablesClear(); err != nil {
		Fatal(ExitGeneralError, "Ошибка: не удалось очистить правила: %s", err)
	}

	// Запускаем testers и nfqws procs
	type Tester struct {
		Cgroup osutil.Cgroup
		Req    nfqws.Req
		Result []domains.Domain // json
	}

	stratsAll, err := os.ReadDir(cfg.stratsDir)
	if err != nil {
		Fatal(ExitGeneralError, "Ошибка: не удалось прочитать стратегии из директории %s: %s\n", cfg.stratsDir, err)
	}

	resultCh := make(chan Tester, len(stratsAll))
	nfqwsReqCh := make(chan nfqws.Req)             // Чан запросов без буффера
	nfqwsErrCh := make(chan error, len(stratsAll)) // Все ошибки в буфере, выводим по завершении

	nfqwsPath := getNfqwsBinPath()
	manager := nfqws.NewManager(nfqwsPath, nfqwsReqCh, nfqwsErrCh)
	manager.Start()
	Recoverer.Add(func() error {
		// Выводим ошибки завершения nfqws.Manager
		go func() {
			for err := range nfqwsErrCh {
				if err != nil {
					fmt.Println("Ошбка завершения nfqws", err)
				}
			}
		}()
		manager.Stop()
		return nil
	}) // TODO: не соответствует логике Recoverer

	getUnDomainsFile, err := uniqueFileGen(tmpDomainsDir, tmpDomainsName)
	if err != nil {
		Fatal(ExitIO, "Ошибка при открытии/чтении файла с доменами: %s", err)
	}

	scopesNames := genScopesNames()
	ctx, cancel := context.WithCancel(context.Background())
	// Ожидается, что zapretThreads > 0
	// Горутина спавнит горутины-тестеры, ожидающе осовобождения nfqws процесса
	// Ожидать одновременно могут до (maxThreads - cfg.zapretThreads) горутин
	// Тестеры будут заменять выполнившиеся, не меняя имени
	// Завершается по сигналу ctx.Done или после завершения всех горутин
	//
	// Если хоть один тестер вернет ошибку - убиваем всех.
	Recoverer.Add(func() error { return osutil.KillCGroup(osutil.CGroupLynxSystemdPath, cgroupSliceName) })
	go func() {
		// TODO: минимально рабочий вариант, улучшить
		// TODO: разбить на функции, упростить код
		// var CGwg sync.WaitGroup
		var wgCounter int32
		var mu sync.Mutex
		defer func() {
			var counter int32
			for {
				select {
				case <-ctx.Done():
					// Все процессы к которым привязаны горутины будут уничтоены на уровне ос, в канал отправится пустой результат
					osutil.KillCGroup(osutil.CGroupLynxSystemdPath, cgroupSliceName)
					close(resultCh)
					return
				default:
					counter = atomic.LoadInt32(&wgCounter)
					if counter == 0 {
						close(resultCh)
						return
					}
				}
			}
		}()
		defer fmt.Println("Жду сигнала остановки или завершения горутин")

		qnums := genQnums()
		nfqwsInstanses := make(chan struct{}, cfg.zapretThreads) // Семафор
		for _, str := range stratsAll {
			// fmt.Printf("Итерация горутины:\n\tq=%v, strat=%v\n", q, str)
			select {
			case <-ctx.Done():
				fmt.Println("Горутина завершена по сигналу cancel")
				return
			default:
				nfqwsInstanses <- struct{}{} // Если буфер семаформа свободен -> в слайсе есть свободные очереди
				mu.Lock()
				qnum := qnums[0]
				qnums = qnums[1:]
				mu.Unlock()

				unDomainsFile := getUnDomainsFile(qnum) // Создаем разные файлы с доменами для каждого тестера
				if unDomainsFile == "" {
					Fatal(ExitIO, "Ошибка: не удалось заполнить доменами временный файл в %s: %s", tmpDomainsDir, err)
				}

				strPath := filepath.Join(cfg.stratsDir, str.Name())
				tester := Tester{Req: nfqws.Req{Queue: qnum, Args: strPath}}

				// CGwg.Add(1)
				atomic.AddInt32(&wgCounter, 1)
				go func(qnum int) {
					// defer CGwg.Done()
					defer func() {
						mu.Lock()
						qnums = append(qnums, qnum)
						mu.Unlock()
						atomic.AddInt32(&wgCounter, -1)
						<-nfqwsInstanses
					}()
					// Запрашиваем у manager запуск nfqws, ждем результат
					nfqwsReqCh <- tester.Req
					if err := <-nfqwsErrCh; err != nil {
						Fatal(ExitGeneralError, "Ошибка: не удалось запустить новый процесс nfqws: %s", err)
					}
					// -with-file - запускаем только после генерации таблицы
					fmt.Printf("Итерация горутины: запускаю tester\n")
					tester.Cgroup = osutil.NewCGroupScope(cgroupSliceName, scopesNames[qnum-startQueueNum],
						cfg.testerBin, "-file", unDomainsFile, "-with-file", readyFilePath, "-file-timeout", "20")
					// Для всех игнорируем ошибки
					// они пеехватятся в nftables или в <-ResChan
					fmt.Printf("Cgroup завершена, tester: \nSLICE: %v, %v\nSTRAT: %v\n ERR: %v\n",
						tester.Cgroup.Slice, tester.Cgroup.Unit, tester.Req.Args, tester.Cgroup.Err)
					resultCh <- tester
				}(qnum)
			}
		}
	}()
	Recoverer.Add(func() error {
		cancel()
		return nil
	}) // TODO: не соответствует логике Recoverer

	// TODO: удаление временных файлов

	err = firewall.NftablesApply(nftTablePattern)
	if err != nil {
		Fatal(ExitGeneralError, "Ошибка: не удалось применить шаблон nftables: %s\n", err)
	}
	// Ждем создания всех cgroup
	// TODO: горутины для всех процессов
	func() {
		lastScope := fmt.Sprintf(scopePathPattern,
			cgroupSliceName, scopesNames[len(scopesNames)-1]) // Длина не меньше 1
		lastScopePath := filepath.Join(osutil.CGroupLynxSystemdPath, lastScope)
		for range 60 { // 5 сек
			if err := osutil.IsFileExist(lastScopePath, ""); err == nil {
				break
			}
			time.Sleep(1 * time.Second)
		}
	}()
	// Дополняем таблицу правилами перенаправления трафика
	err = nftableGenRules(scopesNames)
	if err != nil {
		Fatal(ExitGeneralError, "Ошибка: не удалось установить временные правила: %s\n", err)
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

	for t := range resultCh {
		fmt.Println(string(t.Cgroup.Out))
	}
}

// ProgramEnd выполняет все отложенные в Recoverer функции, устанавлиавет код выхода == OsExit
func ProgramEnd() {
	if err := Recoverer.RecoverAll(); err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка при восстановлении системы: %v", err)
	}
	os.Exit(OsExit)
}

// Fatal выводит форматированное сообщение в stderr и устанавливает код ошибки
func Fatal(exitCode int, msg string, args ...any) {
	fmt.Fprintf(os.Stderr, msg, args...)
	OsExit = exitCode
	ProgramEnd()
}

// checkDeps проверяет наличие необходимых файлов и программ в системе:
// zappret, tester, nftables
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

// getNfqwsBinPath возврашает путь к бинарнику nfqws в зависимости от системы. Заглушка
func getNfqwsBinPath() string {
	// TODO: автоопределение бинарника для каждой платформы
	return filepath.Join(cfg.zapretFolder, "binaries/linux-x86_64/nfqws")
}

// uniqueFileGen возвращает функцию, которая принимает аргументы форматирования и
// созает файл с названием в требуемом формате в переданной родительской функции директории.
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

	startLine := 0
	step := len(lines) * cfg.zapretThreads / 10
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
		f, err := os.OpenFile(fPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
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

// genScopesNames на основе кол-ва потоков zapret генерирует слайс-список названий cgroup.
// В случае, когда cfg.zapretThreads < 1, вернет пустой слайс
func genScopesNames() []string {
	names := make([]string, 0, cfg.zapretThreads)
	for q := range cfg.zapretThreads {
		name := fmt.Sprintf(cgroupScopeName, q+startQueueNum)
		names = append(names, name)
	}
	return names
}

// genQnums на основе кол-ва потоков zapret генерирует слайс-список значений qnum.
// В случае, когда cfg.zapretThreads < 1, вернет пустой слайс
func genQnums() []int {
	slice := make([]int, 0, cfg.zapretThreads)
	for q := range cfg.zapretThreads {
		slice = append(slice, startQueueNum+q)
	}
	return slice
}
