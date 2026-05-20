package worklog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Logger struct {
	mu  sync.Mutex
	dir string
}

type Entry struct {
	Title        string
	UserRequest  string
	Assistant    string
	Summary      []string
	ChangedFiles []string
	Verification []string
}

func NewDefault() *Logger {
	return &Logger{dir: defaultLogDir()}
}

func New(dir string) *Logger {
	return &Logger{dir: dir}
}

func (l *Logger) Append(entry Entry) error {
	if l == nil {
		return nil
	}
	loc := chinaLocation()
	now := time.Now().In(loc)
	date := now.Format("20060102")
	path := filepath.Join(l.dir, date+".MD")

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(fmt.Sprintf("# AI工作记录 - %s\n\n## 今日摘要\n- 自动创建当天记录文件。\n", date)), 0o644); err != nil {
			return err
		}
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(formatEntry(now, entry))
	return err
}

func (l *Logger) PathForToday() string {
	loc := chinaLocation()
	return filepath.Join(l.dir, time.Now().In(loc).Format("20060102")+".MD")
}

func formatEntry(ts time.Time, entry Entry) string {
	title := strings.TrimSpace(entry.Title)
	if title == "" {
		title = "CodexFlow 同步记录"
	}
	userRequest := strings.TrimSpace(entry.UserRequest)
	if userRequest == "" {
		userRequest = "无"
	}
	assistant := strings.TrimSpace(entry.Assistant)
	if assistant == "" {
		assistant = "无"
	}

	return fmt.Sprintf(`

## 进度追加（%s）
### 任务：%s
- 用户要求：
`+"```text\n%s\n```\n"+`- 助手回复：
`+"```text\n%s\n```\n"+`- 执行摘要：
%s
- 变更文件：
%s
- 验证结果：
%s
`, ts.Format("2006-01-02 15:04:05 CST"), title, userRequest, assistant, bullets(entry.Summary, "无"), bullets(entry.ChangedFiles, "无"), bullets(entry.Verification, "未执行"))
}

func bullets(values []string, fallback string) string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return "- " + fallback
	}
	for idx, value := range cleaned {
		cleaned[idx] = "- " + value
	}
	return strings.Join(cleaned, "\n")
}

func defaultLogDir() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return "AI工作记录"
	}
	local := filepath.Join(wd, "AI工作记录")
	if _, err := os.Stat(local); err == nil {
		return local
	}
	parent := filepath.Join(filepath.Dir(wd), "AI工作记录")
	if _, err := os.Stat(parent); err == nil {
		return parent
	}
	return local
}

func chinaLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*60*60)
}

func Truncate(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "\n...（已截断）"
}
