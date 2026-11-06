#!/bin/bash

# Pre-commit проверки
# Этот скрипт должен выполняться перед каждым коммитом

set -euo pipefail  # Строгий режим: -e (exit on error), -u (undefined vars), -o pipefail (pipe errors)

START_TIME=$(date +%s)
echo "Запуск pre-commit-lint проверок..."
echo "Время начала: $(date)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

WARNING_COUNT=0
WARNING_MESSAGES=()

print_info() {
    echo -e "${BLUE}ℹ️ $1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    WARNING_COUNT=$((WARNING_COUNT + 1))
    WARNING_MESSAGES+=("$1")
    echo -e "${YELLOW}⚠️ $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

measure_time() {
    local start_time=$1
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    echo "Время выполнения: ${duration}с"
}

check_tool() {
    local tool=$1
    local install_cmd=$2
    if ! command -v "$tool" >/dev/null 2>&1; then
        print_warning "$tool не установлен. Установите: $install_cmd"
        return 1
    fi
    return 0
}

load_env() {
    if [ ! -f .env ]; then
        return 0
    fi
    echo "Загружаем переменные из .env файла"
    set -a
    while IFS='=' read -r key value; do
        if [ -n "$key" ] && [ -n "$value" ]; then
            export "$key=$value"
        fi
    done < <(grep -v '^#' .env | grep -v '^$' | tr -d '\r')
    set +a
}

# 1. Проверка форматирования кода
STEP_START=$(date +%s)
print_info "Проверка форматирования кода..."
UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
    print_error "Код не отформатирован. Запустите: gofmt -w ."
    echo "$UNFORMATTED"
    exit 1
fi
print_success "Код отформатирован правильно"
measure_time $STEP_START

# 2. Проверка линтера
STEP_START=$(date +%s)
print_info "Проверка линтера..."
if ! golangci-lint run ./...; then
    print_error "Линтер обнаружил проблемы"
    exit 1
fi
print_success "Линтер прошел успешно"
measure_time $STEP_START

# 3. Проверка импортов
STEP_START=$(date +%s)
print_info "Проверка импортов..."
if check_tool "goimports" "go install golang.org/x/tools/cmd/goimports@latest"; then
    UNSORTED_IMPORTS=$(goimports -l $(go list -f '{{.Dir}}' ./...) || true)
    if [ -n "$UNSORTED_IMPORTS" ]; then
        print_warning "Некоторые импорты не отсортированы:"
        echo "$UNSORTED_IMPORTS"
    else
      print_success "Импорты отсортированы правильно"
    fi
fi

measure_time $STEP_START

# 4. Запуск тестов
STEP_START=$(date +%s)
print_info "Запуск тестов..."
if [ -f .env ]; then
   load_env

    if ! go test -v ./...; then
        print_error "Тесты не прошли"
        exit 1
    fi
    print_success "Все тесты прошли успешно (с переменными окружения)"
else
    echo "Файл .env не найден, запускаем только unit тесты"
    UNIT_PACKAGES=$(go list ./... | grep -v '/tests/integration' || true)
    if [ -n "$UNIT_PACKAGES" ]; then
        if ! go test -v $UNIT_PACKAGES; then
            print_error "Unit тесты не прошли"
            exit 1
        fi
        print_success "Unit тесты прошли успешно (интеграционные тесты пропущены - нет .env файла)"
    else
        print_warning "Нет unit тестов для запуска"
    fi
fi
measure_time $STEP_START

# 5. Проверка покрытия тестами
STEP_START=$(date +%s)
print_info "Проверка покрытия тестами..."
if [ -f .env ]; then
    load_env

    mkdir -p coverage
    go test -coverprofile=coverage/coverage.out -covermode=atomic ./...
    go tool cover -html=coverage/coverage.out -o coverage/coverage.html

    COVERAGE=$(go tool cover -func=coverage/coverage.out | grep '^total:' | awk '{print $3}' | sed 's/%//')
else
    echo "Файл .env не найден, проверяем покрытие только unit тестов"
    UNIT_PACKAGES=$(go list ./... | grep -v '/tests/integration' || true)
    if [ -n "$UNIT_PACKAGES" ]; then
        mkdir -p coverage
        go test -coverprofile=coverage/coverage.out -covermode=atomic $UNIT_PACKAGES
        go tool cover -html=coverage/coverage.out -o coverage/coverage.html
        COVERAGE=$(go tool cover -func=coverage/coverage.out | grep '^total:' | awk '{print $3}' | sed 's/%//')
    else
        COVERAGE="0"
    fi
fi

MIN_COVERAGE=${MIN_COVERAGE:-50}
if (( $(echo "$COVERAGE < $MIN_COVERAGE" | bc -l) )); then
    print_warning "Покрытие тестами низкое: ${COVERAGE}% (минимум: ${MIN_COVERAGE}%)"
    echo "Отчет о покрытии: coverage/coverage.html"
else
    print_success "Покрытие тестами: ${COVERAGE}%"
    echo "Отчет о покрытии: coverage/coverage.html"
fi
measure_time $STEP_START

# 6. Проверка сборки приложения
STEP_START=$(date +%s)
print_info "Проверка сборки приложения..."
mkdir -p build
if ! go build -ldflags="-s -w" -o build cmd/lman/main.go; then
    print_error "Основное приложение не собирается"
    exit 1
fi
print_success "Основное приложение собирается успешно"
rm build/main
measure_time $STEP_START

# 7. Проверка зависимостей
STEP_START=$(date +%s)
print_info "Проверка зависимостей..."

if ! git diff --quiet go.mod go.sum 2>/dev/null; then
    print_warning "Обнаружены изменения в go.mod или go.sum"
    echo "Запускаем go mod tidy..."
    if ! go mod tidy; then
        print_error "Проблемы с зависимостями"
        exit 1
    fi

    if ! git diff --quiet go.mod go.sum 2>/dev/null; then
        print_error "go mod tidy внес изменения. Пожалуйста, закоммитьте их:"
        git diff go.mod go.sum
        exit 1
    fi
else
  print_success "Зависимости в порядке"
fi

measure_time $STEP_START

# 8. Проверка уязвимостей в зависимостях
STEP_START=$(date +%s)
print_info "Проверка уязвимостей в зависимостях..."

if ! check_tool "govulncheck" "go install golang.org/x/vuln/cmd/govulncheck@latest"; then
    print_error "govulncheck не установлен"
    exit 1
fi
if ! govulncheck ./...; then
    print_error "Обнаружены уязвимости в зависимостях"
    exit 1
fi
print_success "Уязвимости в зависимостях не найдены"

measure_time $STEP_START

# 9. Проверка безопасности
STEP_START=$(date +%s)
print_info "Проверка безопасности..."

if ! check_tool "gosec" "go install github.com/securego/gosec/v2/cmd/gosec@latest"; then
    print_error "gosec не установлен"
    exit 1
fi
mkdir -p security
if ! gosec -fmt json -out security/gosec-report.json -exclude-dir=generated ./...; then
    print_error "Обнаружены проблемы безопасности. Отчет: security/gosec-report.json"
    exit 1
fi
print_success "Проверка безопасности прошла"

measure_time $STEP_START

# Финальная статистика
echo ""
echo "📊 Статистика:"
echo "   - Время выполнения: $(measure_time $START_TIME | cut -d' ' -f3-)"
echo "   - Отчет о покрытии: coverage/coverage.html"
echo "   - Отчет о безопасности: security/gosec-report.json"
if [ "$WARNING_COUNT" -gt 0 ]; then
    print_error "Найдено предупреждений: ${WARNING_COUNT}. Коммит лучше не делать, пока они не исправлены."
    for msg in "${WARNING_MESSAGES[@]}"; do
        echo -e "${YELLOW}      • ${msg}${NC}"
    done
    exit 1
fi

print_success "Все проверки прошли успешно!"
echo ""
echo "Готово к коммиту."
