package wrapper

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const MaxSkillsUpload = 16 << 20
const maxSkillFile = 1 << 20
const maxSkillTotal = 32 << 20

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ParseSkillMarkdown 解析标准 YAML frontmatter 和 Markdown 指令。
func ParseSkillMarkdown(raw []byte, directory string) (SkillDef, error) {
	if len(raw) > maxSkillFile || !utf8.Valid(raw) {
		return SkillDef{}, fmt.Errorf("SKILL.md must be UTF-8 and at most 1 MiB")
	}
	text := strings.ReplaceAll(strings.TrimPrefix(string(raw), "\uFEFF"), "\r\n", "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if !strings.HasPrefix(text, "---\n") {
		return SkillDef{}, fmt.Errorf("SKILL.md requires YAML frontmatter")
	}
	front, body, found := strings.Cut(text[4:], "\n---\n")
	if !found {
		return SkillDef{}, fmt.Errorf("SKILL.md frontmatter closing delimiter is missing")
	}
	var header struct {
		Name          string            `yaml:"name"`
		Description   string            `yaml:"description"`
		License       string            `yaml:"license"`
		Compatibility string            `yaml:"compatibility"`
		Metadata      map[string]string `yaml:"metadata"`
		AllowedTools  string            `yaml:"allowed-tools"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(front))
	decoder.KnownFields(true)
	if err := decoder.Decode(&header); err != nil {
		return SkillDef{}, fmt.Errorf("invalid skill frontmatter: %w", err)
	}
	var syntax yaml.Node
	if err := yaml.Unmarshal([]byte(front), &syntax); err != nil {
		return SkillDef{}, err
	}
	if len(syntax.Content) == 0 || syntax.Content[0].Kind != yaml.MappingNode {
		return SkillDef{}, fmt.Errorf("skill frontmatter must be a mapping")
	}
	fields := syntax.Content[0].Content
	resolve := func(node *yaml.Node) *yaml.Node {
		if node.Kind == yaml.AliasNode {
			return node.Alias
		}
		return node
	}
	for i := 0; i < len(fields); i += 2 {
		key, value := fields[i].Value, resolve(fields[i+1])
		if key == "metadata" {
			if value.Kind != yaml.MappingNode {
				return SkillDef{}, fmt.Errorf("metadata must map strings to strings")
			}
			for _, node := range value.Content {
				if resolve(node).Tag != "!!str" {
					return SkillDef{}, fmt.Errorf("metadata keys and values must be strings")
				}
			}
		} else if value.Tag != "!!str" {
			return SkillDef{}, fmt.Errorf("frontmatter field %s must be a string", key)
		}
		if key == "compatibility" && strings.TrimSpace(header.Compatibility) == "" {
			return SkillDef{}, fmt.Errorf("compatibility must be nonempty when provided")
		}
	}
	if len(header.Name) > 64 || !skillNamePattern.MatchString(header.Name) {
		return SkillDef{}, fmt.Errorf("skill name must be 1-64 lowercase letters/numbers with single hyphens")
	}
	if directory != "" && directory != header.Name {
		return SkillDef{}, fmt.Errorf("skill name %q must match its parent directory %q", header.Name, directory)
	}
	if strings.TrimSpace(header.Description) == "" || utf8.RuneCountInString(header.Description) > 1024 {
		return SkillDef{}, fmt.Errorf("skill description must contain 1-1024 characters")
	}
	if utf8.RuneCountInString(header.Compatibility) > 500 {
		return SkillDef{}, fmt.Errorf("skill compatibility exceeds 500 characters")
	}
	return SkillDef{Name: header.Name, Description: header.Description, Instructions: strings.TrimSpace(body), License: header.License, Compatibility: header.Compatibility, Metadata: header.Metadata, AllowedTools: header.AllowedTools, Resources: map[string]SkillResource{}}, nil
}

// ParseSkillsPackage 在内存中读取 ZIP，不把上传的文件解压到宿主文件系统。
func ParseSkillsPackage(raw []byte) ([]SkillDef, error) {
	if len(raw) > MaxSkillsUpload {
		return nil, fmt.Errorf("skill package exceeds 16 MiB")
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("invalid skill ZIP: %w", err)
	}
	if len(archive.File) > 256 {
		return nil, fmt.Errorf("skill package contains more than 256 entries")
	}
	files := map[string][]byte{}
	total := 0
	for _, file := range archive.File {
		name := file.Name
		if strings.ContainsAny(name, "\\\x00:") || strings.HasPrefix(name, "/") || strings.TrimSuffix(name, "/") != path.Clean(name) || name == "." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("unsafe package path %q", name)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.Mode().IsRegular() {
			return nil, fmt.Errorf("non-regular package entry %q", name)
		}
		if _, exists := files[name]; exists {
			return nil, fmt.Errorf("duplicate package path %q", name)
		}
		if file.UncompressedSize64 > maxSkillFile {
			return nil, fmt.Errorf("package file %q exceeds 1 MiB", name)
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxSkillFile+1))
		_ = reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if len(data) > maxSkillFile {
			return nil, fmt.Errorf("package file exceeds 1 MiB")
		}
		total += len(data)
		if total > maxSkillTotal {
			return nil, fmt.Errorf("expanded skill package exceeds 32 MiB")
		}
		files[name] = data
	}
	entries := []string{}
	for name := range files {
		if path.Base(name) == "SKILL.md" {
			entries = append(entries, name)
		}
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		return nil, fmt.Errorf("package must contain SKILL.md")
	}
	skills := []SkillDef{}
	names := map[string]bool{}
	assigned := map[string]bool{}
	for _, entry := range entries {
		directory := path.Dir(entry)
		parent := ""
		if directory != "." {
			parent = path.Base(directory)
		}
		skill, err := ParseSkillMarkdown(files[entry], parent)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry, err)
		}
		if names[skill.Name] {
			return nil, fmt.Errorf("duplicate skill name %q", skill.Name)
		}
		names[skill.Name] = true
		prefix := ""
		if directory != "." {
			prefix = directory + "/"
		}
		for name, data := range files {
			if !strings.HasPrefix(name, prefix) || name == entry {
				continue
			}
			relative := strings.TrimPrefix(name, prefix)
			// 一个技能不能包含另一个技能目录，避免资源归属不明确。
			for _, other := range entries {
				if other != entry && strings.HasPrefix(other, prefix) {
					return nil, fmt.Errorf("nested skill directories are unsupported")
				}
			}
			encoding, content := "utf-8", string(data)
			if !utf8.Valid(data) || bytes.ContainsRune(data, '\x00') {
				encoding = "base64"
				content = base64.StdEncoding.EncodeToString(data)
			}
			skill.Resources[relative] = SkillResource{Encoding: encoding, Content: content}
			assigned[name] = true
		}
		assigned[entry] = true
		skills = append(skills, skill)
	}
	for name := range files {
		if !assigned[name] {
			return nil, fmt.Errorf("package file %q is outside any skill directory", name)
		}
	}
	return skills, nil
}
