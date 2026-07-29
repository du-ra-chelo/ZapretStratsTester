// Package osutil предлагает функции для простого взаимодействия с ОС
package osutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
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

// TODO: cgroup struct?

// NewCGroupScope создает новый unit в cgroup
func NewCGroupScope(slice, unit, prog string, pArgs ...string) CgroupResult {
	cmdArgs := []string{"--scope", "--slice=" + slice, "--unit=" + unit, prog}
	cmdArgs = append(cmdArgs, pArgs...)
	cmd := exec.Command("systemd-run", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		err = fmt.Errorf("systemd-run failed: %w, output: %s", err, out)
	}
	return CgroupResult{slice: slice, unit: unit, err: err}
}

// KillCGroup отправляет SIGKILL всем процессам в slice
func KillCGroup(cgroupHome, sliceName string) error {
	slicePath := filepath.Join(cgroupHome, sliceName+".slice")

	// Проверяем существование
	if _, err := os.Stat(slicePath); err != nil {
		return fmt.Errorf("cgroup не существует: %w", err)
	}

	// Записываем "1" в cgroup.kill (только запись)
	killerPath := filepath.Join(slicePath, "cgroup.kill")
	if err := os.WriteFile(killerPath, []byte("1"), 0o200); err != nil {
		return fmt.Errorf("ошибка записи в cgroup.kill: %w", err)
	}

	// Ждём завершения всех процессов
	procsPath := filepath.Join(slicePath, "cgroup.procs")
	maxWait := 30 * time.Second
	checkInterval := 100 * time.Millisecond
	timeout := time.After(maxWait)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("таймаут ожидания завершения процессов в %s", slicePath)
		default:
			data, err := os.ReadFile(procsPath)
			if err != nil {
				return fmt.Errorf("ошибка чтения cgroup.procs: %w", err)
			}
			// Если файл пуст или содержит только пробелы — процессов нет
			if len(data) == 0 || len(bytes.TrimSpace(data)) == 0 {
				// Удаляем slice
				if err := os.RemoveAll(slicePath); err != nil {
					return fmt.Errorf("ошибка удаления slice: %w", err)
				}
				return nil
			}
			time.Sleep(checkInterval)
		}
	}
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
