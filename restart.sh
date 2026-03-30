#!/bin/bash

echo "🔄 Перезапуск сервера с новыми изменениями"
echo ""

cd "$(dirname "$0")"

echo "1️⃣  Останавливаем старый сервер..."
SERVER_PID=$(lsof -ti:8080 2>/dev/null)
if [ ! -z "$SERVER_PID" ]; then
    kill $SERVER_PID 2>/dev/null
    sleep 2
    echo "   ✅ Сервер на порту 8080 остановлен (PID: $SERVER_PID)"
else
    echo "   ℹ️  Сервер не запущен"
fi

echo ""
echo "2️⃣  Пересборка проекта..."
make clean > /dev/null 2>&1
make build

if [ $? -ne 0 ]; then
    echo "   ❌ Ошибка сборки!"
    exit 1
fi

echo ""
echo "3️⃣  Запуск нового сервера..."
echo "   🌐 Сервер будет доступен на http://localhost:8080"
echo "   📝 Для остановки нажмите Ctrl+C"
echo ""

./bin/server
