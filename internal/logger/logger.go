package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	once     sync.Once
	instance *log.Logger
	logFile  *os.File
	logPath  string
)

// Init sets up file logging in the given directory.
// Logs go to warehouse-ui.log with rotation (keeps last file as .log.1).
func Init(dataDir string) {
	once.Do(func() {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			log.Printf("logger: failed to create dir %s: %v", dataDir, err)
			instance = log.New(os.Stderr, "", log.LstdFlags|log.Lshortfile)
			return
		}

		logPath = filepath.Join(dataDir, "warehouse-ui.log")

		// Rotate if log > 5MB
		if info, err := os.Stat(logPath); err == nil && info.Size() > 5*1024*1024 {
			os.Rename(logPath, logPath+".1")
		}

		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("logger: failed to open %s: %v", logPath, err)
			instance = log.New(os.Stderr, "", log.LstdFlags|log.Lshortfile)
			return
		}
		logFile = f

		// Write to both file and stderr
		w := io.MultiWriter(f, os.Stderr)
		instance = log.New(w, "", log.LstdFlags|log.Lshortfile)

		instance.Printf("=== Warehouse UI started at %s ===", time.Now().Format(time.RFC3339))
	})
}

// Path returns the log file path.
func Path() string {
	return logPath
}

// Close flushes and closes the log file.
func Close() {
	if logFile != nil {
		logFile.Close()
	}
}

func get() *log.Logger {
	if instance == nil {
		return log.Default()
	}
	return instance
}

// Info logs an informational message.
func Info(format string, args ...any) {
	get().Output(2, fmt.Sprintf("[INFO] "+format, args...))
}

// Warn logs a warning.
func Warn(format string, args ...any) {
	get().Output(2, fmt.Sprintf("[WARN] "+format, args...))
}

// Error logs an error.
func Error(format string, args ...any) {
	get().Output(2, fmt.Sprintf("[ERROR] "+format, args...))
}

// Debug logs a debug message.
func Debug(format string, args ...any) {
	get().Output(2, fmt.Sprintf("[DEBUG] "+format, args...))
}
