package worklog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendCreatesRequiredMarkdownBlock(t *testing.T) {
	dir := t.TempDir()
	logger := New(dir)
	if err := logger.Append(Entry{
		Title:       "手机端 CodexFlow 输入同步",
		UserRequest: "生成 PDF",
		Assistant:   "已同步到本机 Agent。",
		Summary:     []string{"已写入。"},
		ChangedFiles: []string{
			"AI工作记录/test.MD",
		},
		Verification: []string{"测试通过。"},
	}); err != nil {
		t.Fatalf("append worklog: %v", err)
	}

	path := filepath.Join(dir, time.Now().In(chinaLocation()).Format("20060102")+".MD")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read worklog: %v", err)
	}
	text := string(payload)
	for _, expected := range []string{
		"## 进度追加（",
		"### 任务：手机端 CodexFlow 输入同步",
		"- 用户要求：",
		"```text\n生成 PDF\n```",
		"- 助手回复：",
		"- 执行摘要：",
		"- 变更文件：",
		"- 验证结果：",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("worklog missing %q in:\n%s", expected, text)
		}
	}
}
