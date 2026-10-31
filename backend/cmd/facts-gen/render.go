package main

import (
	"fmt"
	"strings"
)

// Markdown between these markers is owned by facts-gen. Human prose lives
// outside them and survives regeneration; -check compares only the block.
const (
	autoBegin = "<!-- FACTS:AUTO BEGIN — генерируется backend/cmd/facts-gen, не редактировать вручную -->"
	autoEnd   = "<!-- FACTS:AUTO END -->"
)

// RenderMarkdown produces the body of the FACTS:AUTO block: every metric table.
// commit is stamped inside the block; CI regenerates with the checked-out SHA,
// so a stale document fails -check exactly like openapi-check does.
func RenderMarkdown(rep *Report, commit string) string {
	f := rep.Facts
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<!-- измерено на HEAD `%s` -->\n\n", commit))

	b.WriteString("## Backend / Architecture\n\n")
	b.WriteString("| Метрика | Значение | Определение |\n|---|---|---|\n")
	row(&b, "Пакеты Go (весь backend)", f.GoPackagesTotal, "каталоги с хотя бы одним .go-файлом под backend/")
	row(&b, "Пакеты Go (backend/internal)", f.GoPackagesInternal, "то же, ограничено backend/internal/**")
	row(&b, "Go-файлы без тестов", f.GoFiles, "*.go, кроме *_test.go")
	row(&b, "Go тестовых файлов", f.GoTestFiles, "*_test.go")
	row(&b, "Тестовые функции Go", f.GoTestFuncs, "верхнеуровневые func TestXxx(*testing.T), TestMain исключён")
	row(&b, "Бенчмарки Go", f.GoBenchmarks, "func BenchmarkXxx")
	row(&b, "Fuzz-тесты Go", f.GoFuzzers, "func FuzzXxx")
	row(&b, "Example-тесты Go", f.GoExamples, "func ExampleXxx")
	b.WriteString("\n")

	b.WriteString("## Database\n\n")
	b.WriteString("| Метрика | Значение | Определение |\n|---|---|---|\n")
	row(&b, "Миграций up", f.MigrationsUp, "*.up.sql в backend/internal/migrations")
	row(&b, "Миграций down", f.MigrationsDown, "*.down.sql там же")
	row(&b, "Таблиц БД", f.DBTables, "уникальные имена CREATE TABLE во всех up-миграциях")
	b.WriteString("\n")

	b.WriteString("## Security (RBAC)\n\n")
	b.WriteString("| Метрика | Значение | Определение |\n|---|---|---|\n")
	row(&b, "Константы Permission", f.RbacConsts, "строковые константы типа Permission в internal/rbac/rbac.go")
	row(&b, "Записи каталога", f.RbacCatalog, "элементы map catalog в rbac.go")
	row(&b, "Разрешений в БД (seeded)", f.RbacSeeded, "уникальные коды из INSERT INTO permissions во всех up-миграциях")
	row(&b, "Системных ролей", f.SystemRoles, "кортежи INSERT INTO roles с is_system = true")
	b.WriteString("\n`platform.owner` сидируется в таблице permissions и намеренно отсутствует\nв каталоге прав (назначается через членство в роли, а не по маршрутам).\n\n")

	b.WriteString("## API\n\n")
	b.WriteString("| Метрика | Значение | Определение |\n|---|---|---|\n")
	row(&b, "OpenAPI paths", f.OpenAPIPaths, "ключи objects.paths в docs/api/openapi.json")
	row(&b, "OpenAPI operations", f.OpenAPIOperations, "методы get/post/put/patch/delete внутри paths")
	b.WriteString("\nopenapi.json генерируется из живого роутера (`cmd/openapi-gen`), поэтому\nсчётчик отражает код, а CI-гейт `openapi-check` не даёт документу устареть.\n\n")

	b.WriteString("## Frontend\n\n")
	b.WriteString("| Метрика | Значение | Определение |\n|---|---|---|\n")
	row(&b, "Файлы src (.ts/.tsx)", f.FESrcFiles, "frontend/src/**, включая тестовые")
	row(&b, "Тестовые файлы src", f.FETestFiles, "*.test.ts(x)/.spec.ts(x) в frontend/src")
	row(&b, "Объявления it()/test() в src", f.FETestDecls, "запускаемые кейсы vitest; test.describe вычитается")
	row(&b, "Файлы страниц", f.FEPageFiles, "компоненты в frontend/src/pages (без admin/, без тестов)")
	row(&b, "Тесты страниц", f.FEPageTests, "*.test.tsx рядом со страницами")
	row(&b, "Маршрутов <Route path>", f.FERoutes, "объявления в frontend/src/App.tsx (регистр маршрутизации SPA)")
	row(&b, "E2E спецификации", f.E2EFiles, "*.spec.ts в frontend/e2e")
	row(&b, "E2E тесты", f.E2ETests, "объявления test() Playwright; test.describe вычитается")
	b.WriteString("\n")

	b.WriteString("## CI/CD\n\n")
	b.WriteString("| Метрика | Значение | Определение |\n|---|---|---|\n")
	row(&b, "Workflow-файлов", f.CIWorkflows, "*.yml/*.yaml в .github/workflows")
	row(&b, "Jobs суммарно", f.CIJobs, "ключи двухпробельного отступа внутри секции jobs:")
	b.WriteString("\nСписок заданий: ")
	b.WriteString(strings.Join(f.CIJobNames, ", "))
	b.WriteString(".\n\n")

	b.WriteString("## Consistency flags\n\n")
	b.WriteString("| Проверка | Результат |\n|---|---|\n")
	mdFlag(&b, "константы RBAC == каталог", rep.Consistency.RbacConstsMatchCatalog)
	mdFlag(&b, "seeded разрешения == каталог (кроме platform.owner)", rep.Consistency.RbacSeededMatchCatalog)
	mdFlag(&b, "миграции up/down симметричны", rep.Consistency.MigrationPairsSymmetric)
	mdFlag(&b, "маршруты App.tsx уникальны", rep.Consistency.RoutePathsUnique)
	b.WriteString("\n")

	b.WriteString(autoEnd + "\n")
	return b.String()
}

func row(b *strings.Builder, name string, value int, def string) {
	b.WriteString(fmt.Sprintf("| %s | **%d** | %s |\n", name, value, def))
}

func mdFlag(b *strings.Builder, name string, ok bool) {
	v := "❌ рассогласование"
	if ok {
		v = "✅ ok"
	}
	b.WriteString(fmt.Sprintf("| %s | %s |\n", name, v))
}

// MetricsBlockMatch reports whether the committed document's FACTS:AUTO block
// agrees with a freshly rendered one on every metric value line. The header
// (commit stamp/date) outside the block is intentionally not compared.
func MetricsBlockMatch(committed, rendered string) bool {
	cb, err1 := extractBlock(committed)
	rb, err2 := extractBlock(rendered)
	if err1 != nil || err2 != nil {
		return false
	}
	return cb == rb
}

func extractBlock(s string) (string, error) {
	i := strings.Index(s, autoBegin)
	j := strings.Index(s, autoEnd)
	if i < 0 || j < 0 || j <= i {
		return "", fmt.Errorf("маркеры FACTS:AUTO не найдены")
	}
	return s[i+len(autoBegin) : j], nil
}
