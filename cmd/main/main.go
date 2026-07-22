// ZapretStratsTester - программа для параллельного тестирования стратегий на разных экземплярах zapret (nfqws)
// В качестве тестера предусмотрен модуль areAvailable, отправляющий параллельные запросы на домены из списка
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"ZapretStratsTester/internal/osutil"
)

func main() {
	// Проверка прав пользователя
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "Ошибка: требуется запуск с правами root")
		os.Exit(1)
	}
	// Наличие зависимостей: zapret, tester, nftables|iptables TODO
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

	// Сохраняем таблицу nft во временный файл в домашней дирректории
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
		"перекомпилируйте программу или укажите верный путь"); err != nil {
		return err
	}

	// Проверяем наличие firewall
	// По дефолту nftables
	// TODO поддержка iptables и чтение используемого файервола из конфига zapret
	cmd := exec.Command("nft", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("не удалось запустить nftables: %w", err)
	}
	return nil
}
