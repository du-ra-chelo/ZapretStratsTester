// ZapretStratsTester - программа для параллельного тестирования стратегий на разных экземплярах zapret (nfqws)
// В качестве тестера предусмотрен модуль areAvailable, отправляющий параллельные запросы на домены из списка
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"ZapretStratsTester/internal/firewall"
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
	}

	// Сохраняем таблицу nft во временный файл
	// TODO: iptables
	if err := firewall.NftablesSave(); err != nil {
		fatal(ExitGeneralError, "Ошибка: нe удалось создать бэкап таблицы: %s\n", err)
		return
	}
	defer firewall.NftablesRecover() // Таблица восстановится ДО перезапуска zapret

	// Запускаем тестеры для инициализации cgroup
	// Для нестандартных тестеров без ожидания файла-сигнала можно создать
	// sleep infinity -> создать таблицу -> заменить sleep infinity на tester
	var wg sync.WaitGroup
	stratsAll, err := os.ReadDir(cfg.stratsDir)
	if err != nil {
		fatal(ExitGeneralError, "Ошибка: не удалось прочитать стратегии из директории %s: %s\n", cfg.stratsDir, err)
		return
	}
	// Если в папке со стратегиями содежатся файлы неверного содержания, будет выведено сообщение
	// Выполнение продолжится со следующего корректного файла
	zapretInstanses := make(chan struct{}, cfg.zapretThreads) // Семафор
	resultCh := make(chan osutil.CgroupResult, len(stratsAll))

	scopesNames := getScopesNames()
	// Остальные тестеры будут заменять выполнившиеся, не меняя имени
	for _, scopeName := range scopesNames {
		wg.Add(1)
		zapretInstanses <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-zapretInstanses }()
			// TODO: Генерация файлов с доменами в /tmp
			resultCh <- osutil.NewCGroupScope(cgroupSliceName, scopeName,
				cfg.testerBin, "-file", cfg.domainsFile, "-with-file", readyFilePath)
		}()
	}
	// В случае остановки программы все процессы останавливаются, слайс удаляется
	defer osutil.KillCGroup(cgroupHome, cgroupSliceName)

	err = firewall.NftablesApply(nftTablePattern)
	if err != nil {
		fatal(ExitGeneralError, "Ошибка: не удалось применить шаблон nftables: %s\n", err)
		return
	}
	// Ждем создания всех cgroup
	func() {
		lastScope := fmt.Sprintf(scopePathPattern,
			cgroupSliceName, scopesNames[len(scopesNames)-1]) // Длина не меньше 1
		lastScopePath := filepath.Join(cgroupHome, lastScope)
		for range 6 { // 5 сек
			if err := osutil.IsFileExist(lastScopePath, ""); err == nil {
				break
			}
			time.Sleep(1 * time.Second)
		}
	}()
	// Дополняем таблицу правилами перенаправления трафика
	err = nftableGenRules(scopesNames)
	if err != nil {
		fatal(ExitGeneralError, "Ошибка: не удалось установить временные правила: %s\n", err)
		return
	}
	fmt.Println(string(result))
}

func fatal(exitCode int, msg string, args ...interface{}) {
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

func getScopesNames() []string {
	if cfg.zapretThreads < 1 {
		panic("Неверное кол-во потоков")
	}
	scopesNames := make([]string, 0, cfg.zapretThreads)
	for n := 1; n <= cfg.zapretThreads; n++ {
		scopesNames = append(scopesNames, fmt.Sprintf("%s%d", cgroupScopeName, n))
	}
	return scopesNames
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
