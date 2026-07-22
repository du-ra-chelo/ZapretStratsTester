package main

import (
	"log"

	"ZapretStratsTester/internal/osutil"
)

func main() {
	// Проверяем наличие zapret1 в сиситеме
	if err := osutil.IsFileExist(cfg.zapretFolder,
		"Установите zapret или укажите верный путь"); err != nil {
		log.Fatal(err)
	}
	// Проверяем существует ли программа-тестер
	if err := osutil.IsFileExist(cfg.testerBin,
		"Перекомпилируйте программу или укажите верный путь"); err != nil {
		log.Fatal(err)
	}
	// Пробуем завершить системный Zapret, если тот запущен
	if active, err := osutil.IsServiceActive("zapret"); err != nil {
		log.Fatal(err) // Серивис не существует или непредвиденная ошибка systemctl
	} else if active {
		if err := osutil.Systemctl("stop", "zapret"); err != nil { // Пробуем остановить
			log.Fatal(err)
		}
		defer osutil.Systemctl("start", "zapret") // По окончании восстанавливаем состояние
	}
}
