package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

var (
	logInst *log.Logger
	once    sync.Once
)

const (
	colorReset = "\033[0m"
	colorDebug = "\033[36m"
	colorInfo  = "\033[37m"
	colorSucc  = "\033[32m"
	colorWarn  = "\033[33m"
	colorErr   = "\033[31m"
)

const (
	errLogDir        = "logs"
	errLogSuffix     = ".err.log"
	maxErrorFileSize = 10 * 1024 * 1024 // 10MB
)

var errW = &errorWriter{}

type errorWriter struct {
	mu      sync.Mutex
	f       *os.File
	size    int64
	curDate string // "YYYY-MM-DD"
	index   int    // 当天编号，从 1 开始
}

func (w *errorWriter) write(line string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	_ = os.MkdirAll(errLogDir, 0o755)

	today := time.Now().Format("2006-01-02")

	if w.f == nil || w.curDate != today {
		w.curDate = today
		w.index = 0
		if err := w.openNext(); err != nil {
			return // 文件打开失败则抛弃日志
		}
	}

	// 达到上限则滚动
	if w.size+int64(len(line)) > maxErrorFileSize {
		if err := w.openNext(); err != nil {
			return
		}
	}
	n, err := w.f.WriteString(line)
	w.size += int64(n)
	if err != nil {
		// 写失败尝试切下一个文件；下次再写
		_ = w.rotate()
	}
}

func (w *errorWriter) openNext() error {
	// 关闭旧文件
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	// 找到今天可用的文件：若存在且未满则续写，否则递增编号创建新文件
	for {
		w.index++
		name := fmt.Sprintf("%s-%03d%s", w.curDate, w.index, errLogSuffix)
		full := filepath.Join(errLogDir, name)

		if st, err := os.Stat(full); err == nil {
			// 已存在
			if st.Size() < maxErrorFileSize {
				f, err2 := os.OpenFile(full, os.O_APPEND|os.O_WRONLY, 0o644)
				if err2 != nil {
					continue // 打不开就试下一个编号
				}
				w.f = f
				w.size = st.Size()
				return nil
			}
			// 已满，继续下一个编号
			continue
		}

		// 不存在：创建
		f, err := os.OpenFile(full, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		w.f = f
		w.size = 0
		return nil
	}
}

func (w *errorWriter) rotate() error {
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	return w.openNext()
}

func Debug(v ...any) {
	output("DEBUG", colorDebug, v...)
}

func Info(v ...any) {
	output("INFO", colorInfo, v...)
}

func Success(v ...any) {
	output("SUCCESS", colorSucc, v...)
}

func Warning(v ...any) {
	output("WARNING", colorWarn, v...)
}

func Error(v ...any) {
	output("ERROR", colorErr, v...)
}

func output(level, clr string, v ...any) {
	initLogger()

	file, line := callerInfo()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprint(v...)

	// 控制台（彩色）
	colored := fmt.Sprintf("%s[%s] %s %s:%d - %s%s",
		clr, level, timestamp, file, line, message, colorReset)
	logInst.Println(colored)

	// 文件（仅 ERROR 落盘；纯文本）
	if level == "ERROR" {
		plain := fmt.Sprintf("[%s] %s %s:%d - %s\n", level, timestamp, file, line, message)
		errW.write(plain)
	}
}

func initLogger() {
	once.Do(func() {
		logInst = log.New(io.MultiWriter(os.Stdout), "", log.Lmsgprefix)
	})
}

func callerInfo() (string, int) {
	_, file, line, ok := runtime.Caller(3)
	if !ok {
		return "???", 0
	}
	return path.Base(file), line
}
