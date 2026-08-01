package osutil

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const CGroupLynxSystemdPath = "/sys/fs/cgroup"

type Cgroup struct {
	Slice string
	Unit  string
	Out   []byte
	Err   error
}

// NewCGroupScope создает новый unit в cgroup
func NewCGroupScope(slice, unit, prog string, pArgs ...string) Cgroup {
	cmdArgs := []string{"--scope", "--slice=" + slice, "--unit=" + unit, prog}
	cmdArgs = append(cmdArgs, pArgs...)
	cmd := exec.Command("systemd-run", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		err = fmt.Errorf("systemd-run failed: %w, output: %s", err, out)
	}
	return Cgroup{Slice: slice, Unit: unit, Out: out, Err: err}
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
