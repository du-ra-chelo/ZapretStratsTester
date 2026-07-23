// ZapretStratsTester - программа для параллельного тестирования стратегий на разных экземплярах zapret (nfqws)
// В качестве тестера предусмотрен модуль areAvailable, отправляющий параллельные запросы на домены из списка
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"ZapretStratsTester/internal/firewall"
	"ZapretStratsTester/internal/osutil"
)

const (
	cgroupSliceName = "ZST"
	cgroupProcName  = "ZST-tester-" // + 1...
	cgroupPath      = "/sys/fs/cgroup"
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

	// Прогнозируем пути cgroup
	// cgroupPaths := predictCGPaths()
}

// checkDeps проверяет наличие необходимых файлов и программ в системе
func checkDeps() error {
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

// predictCGPaths генерирует список cgroup путей для запуска от рута
// nftables игнорирует cgroupPath и сравнивает начиная со слайс дирректории
func predictCGPaths() (cgroupPaths []string) {
	sliceDir := fmt.Sprintf("/%s.slice", cgroupSliceName)

	for n := range cfg.zapretThreads {
		num := fmt.Sprintf("%v", n+1)
		procName := filepath.Join(sliceDir, cgroupProcName+num)
		cgroupPaths = append(cgroupPaths, procName)
	}
	return
}
