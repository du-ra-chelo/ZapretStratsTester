// Package firewall предлагает функции для определения используемого firewall, его настройки
package firewall

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const nftBackupName = "nfttable-backup-byZST"

// TODO: фнункция для определения используемого фаервола

// nftBackupPath возвращает путь до бэкапа таблицы, даже если тот не существует
func nftBackupPath() string {
	return filepath.Join("/tmp", nftBackupName)
}

// NftablesSave сохраняет текущие настройки nft в файл, если настройки пустые - файл содерижит только комментарий
func NftablesSave() error {
	cmd := exec.Command("nft", "list", "ruleset")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	if len(out) == 0 {
		out = []byte("# No ruleset defined\n")
	}

	return os.WriteFile(nftBackupPath(), out, 0o644)
}

// NftablesClear очищает все правила nft
func NftablesClear() error {
	err := exec.Command("nft", "flush", "ruleset").Run()
	return err
}

// NftablesApply применяет переданную строку как таблицу
func NftablesApply(script string) error {
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft apply: %w\nOutput: %s", err, out)
	}
	return nil
}

// NftablesExec извлекает любую последовательность аргументов для nft
func NftablesExec(command string) error {
	cmd := exec.Command("nft", command)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft apply: %w\nOutput: %s", err, out)
	}
	return nil
}

// NftablesRecover добавляет таблицу из бэкапа, удаляет существующую
func NftablesRecover() error {
	if err := NftablesClear(); err != nil {
		return fmt.Errorf("удаление временной таблицы nftables: %w", err)
	}
	cmd := exec.Command("nft", "--file", nftBackupPath())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("восстановление таблицы nftables: %w\nOutput: %s", err, out)
	}
	_ = os.Remove(nftBackupPath())
	return nil
}

// TODO: аналогичные функции для iptablles
