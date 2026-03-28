package logger

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	defaultLogDir      = "logs"
	defaultLogFilename = "log.txt"
	DefaultPageSize    = 100
)

var (
	Logger *zap.SugaredLogger
	atom   zap.AtomicLevel
)

var levelMap = map[int]zapcore.Level{
	5: zapcore.DebugLevel,
	4: zapcore.InfoLevel,
	3: zapcore.WarnLevel,
	2: zapcore.ErrorLevel,
}

var (
	DefaultLogger *zap.SugaredLogger
	NopLogger     *zap.SugaredLogger

	currentLogFile string
)

func init() {
	atom = zap.NewAtomicLevel()
	NopLogger = newNopLogger()
	DefaultLogger = newLogger(filepath.Join("", defaultLogDir))
}

func InitLogger(logDir string, ll int) {
	DefaultLogger = newLogger(logDir) // передаём исходный logDir, не добавляя defaultLogDir
	SetLevel(ll)
}

// func defaultLogPath() string {
// 	if runtime.GOOS == "windows" {
// 		programData := os.Getenv("ProgramData")
// 		if programData != "" {
// 			return filepath.Join(programData, "Prometheus1CExporter", defaultLogDir, defaultLogFilename)
// 		}
// 		// fallback: папка logs рядом с исполняемым файлом
// 		exe, _ := os.Executable()
// 		return filepath.Join(filepath.Dir(exe), defaultLogDir, defaultLogFilename)
// 	}
// 	// Linux / Unix
// 	return filepath.Join("/var/log/1c_exporter", defaultLogFilename)
// }

func newLogger(logDir string) *zap.SugaredLogger {
	var logWriter io.Writer

	if isTesting() {
		logWriter = os.Stdout
		currentLogFile = "stdout"
	} else {
		var logPath string
		logPath = logDir
		if logDir == "" {
			logPath = defaultLogDir
		}
		currentLogFile = path.Join(logPath, defaultLogFilename)

		if err := os.MkdirAll(logPath, 0755); err != nil {
			logWriter = os.Stdout
			currentLogFile = "stdout"
		} else {
			logWriter = &lumberjack.Logger{
				Filename:   currentLogFile,
				MaxSize:    10,
				MaxBackups: 10,
				MaxAge:     5,
			}
		}
	}

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.AddSync(logWriter),
		atom,
	)

	return zap.New(core).Sugar()
}

func SetLevel(level int) {
	atom.SetLevel(levelMap[level])
}

func newNopLogger() *zap.SugaredLogger {
	return zap.NewNop().Sugar()
}

func isTesting() bool {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test") {
			return true
		}
	}

	return false
}

func Sync() error {
	if DefaultLogger != nil {
		return DefaultLogger.Sync()
	}
	return nil
}

func GetCurrentLogFile() string {
	return currentLogFile
}

func With(fields ...interface{}) *zap.SugaredLogger {
	if DefaultLogger != nil {
		return DefaultLogger.With(fields...)
	}
	return zap.NewNop().Sugar()
}

// LogEntry представляет одну запись в логе
type LogEntry struct {
	LineNumber int64  // номер строки в файле (начиная с 1)
	Content    string // содержимое строки
}

// ReadConfig конфигурация для чтения логов
type ReadConfig struct {
	StartLine int64 // начальная строка (с 1), если 0 - автоматически
	EndLine   int64 // конечная строка, если 0 - до конца файла
	Count     int   // количество записей (положительное - с начала, отрицательное - с конца)
}

// ReadResult результат чтения логов
type ReadResult struct {
	Entries    []LogEntry // прочитанные записи
	TotalLines int64      // всего строк в файле
	FromLine   int64      // с какой строки начали чтение
	ToLine     int64      // до какой строки дочитали
	HasMore    bool       // есть ли еще записи дальше (для пагинации вперед)
	HasPrev    bool       // есть ли записи до (для пагинации назад)
}

// ReadLogs читает логи из текущего файла
func ReadLogs(cfg *ReadConfig) (*ReadResult, error) {
	filePath := currentLogFile
	if filePath == "" || filePath == "stdout" {
		return nil, fmt.Errorf("no log file available")
	}
	return ReadLogsFromFile(filePath, cfg)
}

// ReadLogsFromFile читает логи из указанного файла
func ReadLogsFromFile(filePath string, cfg *ReadConfig) (*ReadResult, error) {
	if cfg == nil {
		cfg = &ReadConfig{Count: -DefaultPageSize}
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	totalLines, err := countLines(file)
	if err != nil {
		return nil, err
	}

	startLine, endLine := calculateRange(totalLines, cfg)

	entries, err := readLines(file, startLine, endLine)
	if err != nil {
		return nil, err
	}

	var hasMore, hasPrev bool
	if cfg.Count > 0 {
		hasMore = endLine < totalLines
		hasPrev = startLine > 1
	} else {
		hasMore = startLine > 1
		hasPrev = endLine < totalLines
	}

	return &ReadResult{
		Entries:    entries,
		TotalLines: totalLines,
		FromLine:   startLine,
		ToLine:     endLine,
		HasMore:    hasMore,
		HasPrev:    hasPrev,
	}, nil
}

// GetFirst читает первые N записей
func GetFirst(n int) (*ReadResult, error) {
	return ReadLogs(&ReadConfig{Count: n})
}

// GetLast читает последние N записей
func GetLast(n int) (*ReadResult, error) {
	return ReadLogs(&ReadConfig{Count: -n})
}

// GetRange читает записи с start по end
func GetRange(start, end int64) (*ReadResult, error) {
	return ReadLogs(&ReadConfig{
		StartLine: start,
		EndLine:   end,
	})
}

// Самая компактная версия calculateRange
func calculateRange(totalLines int64, cfg *ReadConfig) (int64, int64) {
	if totalLines == 0 {
		return 0, 0
	}

	// Нормализуем Count
	count := cfg.Count
	if count == 0 {
		count = -DefaultPageSize
	}

	// Определяем начальные значения
	startLine := cfg.StartLine
	endLine := cfg.EndLine

	// Если нет явного диапазона, вычисляем из Count
	if startLine <= 0 && endLine <= 0 {
		if count > 0 {
			startLine, endLine = 1, int64(count)
		} else {
			startLine = totalLines + int64(count) + 1
			endLine = totalLines
		}
	}

	// Корректируем границы
	if startLine < 1 {
		startLine = 1
	}
	if startLine > totalLines {
		startLine = totalLines
	}

	if endLine == 0 {
		endLine = totalLines
	}
	if endLine < 1 {
		endLine = 1
	}
	if endLine > totalLines {
		endLine = totalLines
	}

	// Меняем местами если нужно
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}

	return startLine, endLine
}

func countLines(file *os.File) (int64, error) {
	if _, err := file.Seek(0, 0); err != nil {
		return 0, err
	}

	scanner := bufio.NewScanner(file)
	lines := int64(0)
	for scanner.Scan() {
		lines++
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	if _, err := file.Seek(0, 0); err != nil {
		return 0, err
	}

	return lines, nil
}

func readLines(file *os.File, startLine, endLine int64) ([]LogEntry, error) {
	scanner := bufio.NewScanner(file)
	var entries []LogEntry
	currentLine := int64(0)

	for scanner.Scan() {
		currentLine++

		if currentLine < startLine {
			continue
		}

		if currentLine > endLine {
			break
		}

		entries = append(entries, LogEntry{
			LineNumber: currentLine,
			Content:    scanner.Text(),
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// Основные методы логирования
func Debug(args ...interface{}) {
	if DefaultLogger != nil {
		DefaultLogger.Debug(args...)
	}
}

func Debugf(template string, args ...interface{}) {
	if DefaultLogger != nil {
		DefaultLogger.Debugf(template, args...)
	}
}

func Info(args ...interface{}) {
	if DefaultLogger != nil {
		DefaultLogger.Info(args...)
	}
}

func Infof(template string, args ...interface{}) {
	if DefaultLogger != nil {
		DefaultLogger.Infof(template, args...)
	}
}

func Warn(args ...interface{}) {
	if DefaultLogger != nil {
		DefaultLogger.Warn(args...)
	}
}

func Warnf(template string, args ...interface{}) {
	if DefaultLogger != nil {
		DefaultLogger.Warnf(template, args...)
	}
}

func Error(args ...interface{}) {
	if DefaultLogger != nil {
		DefaultLogger.Error(args...)
	}
}

func Errorf(template string, args ...interface{}) {
	if DefaultLogger != nil {
		DefaultLogger.Errorf(template, args...)
	}
}

func Fatal(args ...interface{}) {
	if DefaultLogger != nil {
		DefaultLogger.Fatal(args...)
	}
}

func Fatalf(template string, args ...interface{}) {
	if DefaultLogger != nil {
		DefaultLogger.Fatalf(template, args...)
	}
}
