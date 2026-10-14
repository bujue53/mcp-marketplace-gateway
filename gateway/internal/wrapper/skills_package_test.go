package wrapper

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const skillFixture = "---\nname: report-agent\ndescription: Generate a report from database rows.\nlicense: MIT\ncompatibility: Requires an MCP gateway\nmetadata:\n  author: qa\nallowed-tools: db_query file_write\n---\n# Instructions\nRead references/guide.md and use scripts/render.py to produce a report.\n"

func packageFixture(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.Write([]byte(content))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
func TestSkillPackageResourcesAndPersistence(t *testing.T) {
	skills, err := ParseSkillsPackage(packageFixture(t, map[string]string{"report-agent/SKILL.md": skillFixture, "report-agent/references/guide.md": "Read only SQL", "report-agent/scripts/render.py": "print('report')", "report-agent/assets/icon.bin": "\x00\xff"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Metadata["author"] != "qa" || skills[0].AllowedTools != "db_query file_write" || skills[0].Resources["assets/icon.bin"].Encoding != "base64" {
		t.Fatal(skills)
	}
	raw, _ := json.Marshal(skills)
	var restored []SkillDef
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	adapter := NewSkillsAdapter(restored)
	result, err := adapter.CallTool(context.Background(), "skill_report-agent", []byte(`{}`))
	if err != nil || !strings.Contains(result.Content[0].Text, "references/guide.md") {
		t.Fatal(result, err)
	}
	resource, err := adapter.CallTool(context.Background(), "skill_report-agent", []byte(`{"resource":"scripts/render.py"}`))
	if err != nil || !strings.Contains(resource.Content[0].Text, "print") {
		t.Fatal(resource, err)
	}
	if _, err := adapter.CallTool(context.Background(), "skill_report-agent", []byte(`{"resource":"../outside"}`)); err == nil {
		t.Fatal("outside resource accepted")
	}
}
func TestSkillPackageRejectsInvalidAndUnsafeInputs(t *testing.T) {
	for _, files := range []map[string]string{
		{"report-agent/SKILL.md": strings.Replace(skillFixture, "name: report-agent", "name: Wrong", 1)},
		{"wrong/SKILL.md": skillFixture},
		{"report-agent/SKILL.md": skillFixture, "../outside": "bad"},
		{"report-agent/SKILL.md": skillFixture, "C:/outside": "bad"},
		{"report-agent/SKILL.md": skillFixture, "outside.txt": "orphan"},
		{"a/report-agent/SKILL.md": skillFixture, "b/report-agent/SKILL.md": skillFixture},
		{"report-agent/SKILL.md": strings.Replace(skillFixture, "description: Generate a report from database rows.", "description: ''", 1)},
	} {
		if _, err := ParseSkillsPackage(packageFixture(t, files)); err == nil {
			t.Fatalf("invalid ZIP accepted: %v", files)
		}
	}
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	header := &zip.FileHeader{Name: "report-agent/link"}
	header.SetMode(os.ModeSymlink | 0777)
	file, _ := writer.CreateHeader(header)
	_, _ = file.Write([]byte("../../outside"))
	_ = writer.Close()
	if _, err := ParseSkillsPackage(data.Bytes()); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := ParseSkillMarkdown([]byte("instructions without frontmatter"), ""); err == nil {
		t.Fatal("missing frontmatter accepted")
	}
}
