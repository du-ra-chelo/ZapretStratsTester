// ZapretStratsTester - программа для параллельного тестирования стратегий на разных экземплярах zapret (nfqws)
// В качестве тестера предусмотрен модуль areAvailable, отправляющий параллельные запросы на домены из списка
package main

import (
	"fmt"
	"log"
	"os"
	"sync"

	"ZapretStratsTester/internal/firewall"
	"ZapretStratsTester/internal/osutil"
)

const (
	cgroupSliceName = "ZST"
	cgroupScopeName = "ZST-tester-" // + 1...15
	cgroupHome      = "/sys/fs/cgroup"
	readyFilePath   = "/tmp/nftables-ready"
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

	// Остальные тестеры будут заменять выполнившиеся, не меняя имени
	for n := 1; n <= cfg.zapretThreads; n++ {
		scopeName := fmt.Sprintf("%s%d", cgroupScopeName, n)
		wg.Add(1)
		zapretInstanses <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-zapretInstanses }()
			resultCh <- osutil.NewCGroupScope(cgroupSliceName, scopeName,
				cfg.testerBin, "-file", cfg.domainsFile, "-with-file", readyFilePath)
		}()
	}
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
