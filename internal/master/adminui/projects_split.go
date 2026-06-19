package adminui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mirror-server/internal/config"

	"gopkg.in/yaml.v3"
)

func writeSplitProjectsFile(path string, projectFiles []string, projects config.Projects) error {
	baseDir := filepath.Dir(path)
	currentPaths, err := currentProjectFilePaths(path, projectFiles)
	if err != nil {
		return err
	}
	nextFiles := append([]string(nil), projectFiles...)
	nextProjects := map[string]config.Project{}
	seen := map[string]bool{}
	for _, project := range projects.Projects {
		if seen[project.ID] {
			return fmt.Errorf("项目 id 和中文名称必须存在，且 id 不得重复")
		}
		seen[project.ID] = true
		projectPath := currentPaths[project.ID]
		if projectPath == "" {
			rel, err := defaultProjectFile(project.ID)
			if err != nil {
				return err
			}
			projectPath = filepath.Join(baseDir, filepath.FromSlash(rel))
			if !projectFileEntriesCover(projectFiles, rel) {
				nextFiles = append(nextFiles, rel)
			}
		}
		nextProjects[projectPath] = project
	}
	var removed []string
	for projectID, projectPath := range currentPaths {
		if !seen[projectID] {
			removed = append(removed, projectPath)
			if rel, err := filepath.Rel(baseDir, projectPath); err == nil {
				nextFiles = removeExplicitProjectFile(nextFiles, filepath.ToSlash(rel))
			}
		}
	}
	if err := validateSplitProjects(path, nextFiles, nextProjects); err != nil {
		return err
	}
	if err := replaceSplitProjects(path, nextFiles, nextProjects); err != nil {
		return err
	}
	for _, projectPath := range removed {
		if err := os.Remove(projectPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func currentProjectFilePaths(path string, projectFiles []string) (map[string]string, error) {
	refs, err := config.ResolveProjectFileReferences(path, projectFiles)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, ref := range refs {
		project, err := config.LoadProjectFile(ref.Path, nil)
		if err != nil {
			return nil, err
		}
		out[project.ID] = ref.Path
	}
	return out, nil
}

func validateSplitProjects(path string, projectFiles []string, projects map[string]config.Project) error {
	baseDir := filepath.Dir(path)
	stageDir, err := os.MkdirTemp(baseDir, ".projects-stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)
	entryPath := filepath.Join(stageDir, filepath.Base(path))
	entryData, err := yaml.Marshal(config.Projects{ProjectFiles: projectFiles})
	if err != nil {
		return err
	}
	if err := writeStagedFile(entryPath, entryData); err != nil {
		return err
	}
	for projectPath, project := range projects {
		rel, err := filepath.Rel(baseDir, projectPath)
		if err != nil {
			return err
		}
		if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return errors.New("项目配置文件必须位于 projects.yaml 同目录或子目录")
		}
		data, err := yaml.Marshal(project)
		if err != nil {
			return err
		}
		if err := writeStagedFile(filepath.Join(stageDir, rel), data); err != nil {
			return err
		}
	}
	_, err = config.LoadProjects(entryPath, nil)
	return err
}

func replaceSplitProjects(path string, projectFiles []string, projects map[string]config.Project) error {
	for projectPath, project := range projects {
		data, err := yaml.Marshal(project)
		if err != nil {
			return err
		}
		if err := replaceAdminFile(projectPath, data); err != nil {
			return err
		}
	}
	entryData, err := yaml.Marshal(config.Projects{ProjectFiles: projectFiles})
	if err != nil {
		return err
	}
	return replaceAdminFile(path, entryData)
}

func writeStagedFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func replaceAdminFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	keep = true
	return nil
}

func defaultProjectFile(projectID string) (string, error) {
	if projectID == "" || projectID == "." || projectID == ".." {
		return "", errors.New("新增项目 id 不能作为项目配置文件名")
	}
	for _, r := range projectID {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return "", errors.New("新增项目 id 只能包含英文字母、数字、点、短横线和下划线，才能作为项目配置文件名")
	}
	return filepath.ToSlash(filepath.Join("projects", projectID+".yaml")), nil
}

func projectFileEntriesCover(projectFiles []string, rel string) bool {
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	for _, entry := range projectFiles {
		cleanEntry := filepath.Clean(strings.TrimSpace(entry))
		if cleanEntry == cleanRel {
			return true
		}
		if strings.ContainsAny(cleanEntry, "*?[") {
			if ok, _ := filepath.Match(cleanEntry, cleanRel); ok {
				return true
			}
		}
	}
	return false
}

func removeExplicitProjectFile(projectFiles []string, rel string) []string {
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	out := projectFiles[:0]
	for _, entry := range projectFiles {
		cleanEntry := filepath.Clean(strings.TrimSpace(entry))
		if !strings.ContainsAny(cleanEntry, "*?[") && cleanEntry == cleanRel {
			continue
		}
		out = append(out, entry)
	}
	return out
}
