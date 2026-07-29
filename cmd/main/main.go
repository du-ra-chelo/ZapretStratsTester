// ZapretStratsTester - программа для параллельного тестирования стратегий на разных экземплярах zapret (nfqws)
// В качестве тестера предусмотрен модуль areAvailable, отправляющий параллельные запросы на домены из списка
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"ZapretStratsTester/internal/firewall"
	"ZapretStratsTester/internal/osutil"
)

const (
	cgroupSliceName  = "ZST"
	cgroupScopeName  = "ZST-tester-" // + 1...15
	cgroupHome       = "/sys/fs/cgroup"
	scopePathPattern = "/%s.slice/%s.scope"
	readyFilePath    = "/tmp/nftables-ready"
)

func main() {
	// Проверка прав пользователя
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Ошибка: требуется запуск с правами root")
		os.Exit(1)
	}
	// Наличие зависимостей: zapret, tester, nftables|iptables
	if err := checkDeps(); err != nil {
		log.Fatal(err)
	}
	// Пробуем завершить системный Zapret, если тот запущен
	if active, err := osutil.IsServiceActive("zapret"); err != nil {
		log.Fatal(err) // Серивис не существует или непредвиденная ошибка systemctl
	} else if active {
		if err := osutil.Systemctl("stop", "zapret"); err != nil { // Пробуем остановить
			log.Fatal(err)
		}
		defer osutil.Systemctl("start", "zapret") // По окончании работы восстанавливаем состояние
	}

	// Сохраняем таблицу nft во временный файл
	if err := firewall.NftablesSave(); err != nil {
		log.Fatal("не удалось создать бэкап таблицы: ", err)
	}
	defer firewall.NftablesRecover() // Таблица восстановится ДО перезапуска zapret

	// Запускаем тестеры для инициализации cgroup
	// Для нестандартных тестеров без ожидания файла-сигнала можно создать
	// sleep infinity -> создать таблицу -> заменить sleep infinity на tester
	var wg sync.WaitGroup
	stratsAll, err := os.ReadDir(cfg.stratsDir)
	if err != nil {
		log.Fatal(err)
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
			resultCh <- osutil.NewCGroupScope(cgroupSliceName, scopeName,
				cfg.testerBin, "-file", cfg.domainsFile, "-with-file", readyFilePath)
		}()
	}
	// В случае остановки программы все процессы останавливаются, слайс удаляется
	defer osutil.KillCGroup(cgroupHome, cgroupSliceName)

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
			cfg.wanIface, metaMarkCG, nftTcp, queue) // Правило перенаправления tcp трафика в queue
		err = firewall.NftablesExec(rule)
		if err != nil {
			return fmt.Errorf("ошибка создания правила маршрутизации tcp cgroup: %w", err)
		}
		// UDP
		rule = fmt.Sprintf(nftRulePostnatTemplate,
			cfg.wanIface, metaMarkCG, nftUdp, queue) // Правило перенаправления udp трафика в queue
		err = firewall.NftablesExec(rule)
		if err != nil {
			return fmt.Errorf("ошибка создания правила маршрутизации udp cgroup: %w", err)
		}
		metaMarkCG += metaMarkStep
		queue++
	}
	return nil
}
