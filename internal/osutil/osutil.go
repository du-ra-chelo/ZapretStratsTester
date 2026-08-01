// Package osutil предлагает функции для простого взаимодействия с ОС
package osutil

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Службы, процессы, сервисы

// Systemctl вызывает системный systemctl с указаными аргументами
func Systemctl(args ...string) error {
	// Если скрипт запущен без sudo юзер введет пароль
	cmd := exec.Command("/usr/bin/systemctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %v: %v\nOutput: %s", args, err, out)
	}
	return nil
}

// IsServiceActive проверяет запущен ли сервис. Вызывает systemctl is-active SERVICE --quiet, проверяет код возврата
func IsServiceActive(service string) (bool, error) {
	err := Systemctl("is-active", service, "--quiet")
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		switch exitErr.ExitCode() {
		case 3:
			return false, nil // не активен
		case 4:
			return false, fmt.Errorf("service does not exist")
		default:
			return false, fmt.Errorf("unknown error (code %d): %w", exitErr.ExitCode(), err)
		}
	}
	return false, nil
}

// Файлы

// IsFileExist проверяет существует ли файл, если нет, генерирует ошибку по шаблону + аргумент-решение
func IsFileExist(filename, solution string) error {
	_, err := os.Stat(filename)
	if err != nil {
		msg := fmt.Sprintf("%v не существует или имеет ограниченные права доступа", filename)
		if os.IsNotExist(err) {
			if solution != "" {
				msg += "," + solution
			}
			err = errors.New(msg)
		}
	}
	return err
}

// Программы & утилиты

// IsInstalled проверяет наличие программы в $PATH
func IsInstalled(name string) error {
	_, err := exec.LookPath(name)
	return err
}
