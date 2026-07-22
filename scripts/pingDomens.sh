#!/bin/bash

# Файл с доменами 
FILE="${1:-domains.txt}"

# Проверяем существование файла
if [ ! -f "$FILE" ]; then
  echo "Файл $FILE не найден"
  exit 1
fi

# Читаем домены и пингуем
while IFS= read -r domain; do
  # Пропускаем пустые строки
  [ -z "$domain" ] && continue

  # Пингуем 2 пакета с таймаутом 2 секунды
  if ping -c 2 -W 2 "$domain" >/dev/null 2>&1; then
    echo "$domain - OK"
  else
    echo "$domain - FAIL"
  fi
done <"$FILE"
